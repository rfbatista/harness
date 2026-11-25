package productowner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/application/agentready"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// run is the product owner's follow of one thread's process: the headless
// claude that answers it, from the message that woke it to its idle end.
type run struct {
	threadID  string
	projectID string
	sessionID string
	unsub     func()
	idleMu    sync.Mutex
	idle      *time.Timer

	mu sync.Mutex
	// cur is the answer streaming now and text what has arrived of it.
	cur       *domain.ProductOwnerItem
	text      strings.Builder
	lastSaved time.Time
	// retired is a run the person stopped: what its process still says is
	// dropped.
	retired bool
}

func (s *Service) threadLock(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	return l
}

// update changes a thread's row in one step: fn is applied to the row as it is
// stored now, and the answer says whether it changed anything to save. Every
// write of a thread goes through it, so none saves a stale copy of the whole
// row over another's change, a rename in particular. t, when set, is
// refreshed with the stored row.
func (s *Service) update(ctx context.Context, threadID string, t *domain.ProductOwnerThread, fn func(*domain.ProductOwnerThread) bool) (*domain.ProductOwnerThread, error) {
	s.rowMu.Lock()
	defer s.rowMu.Unlock()
	cur, err := s.store.Thread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if fn(cur) {
		if err := s.store.SaveThread(ctx, *cur); err != nil {
			return nil, err
		}
	}
	if t != nil {
		*t = *cur
	}
	return cur, nil
}

func (s *Service) available() error {
	if s.sessions == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "product owner threads are not available on this server"}
	}
	return nil
}

// Threads implements ports.ProductOwner.
func (s *Service) Threads(ctx context.Context, projectID string) ([]domain.ProductOwnerThread, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	list, err := s.store.Threads(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.PendingHeldActions > 0 {
			// What waited a day expires before the list shows it.
			if n, _ := s.ExpireStale(ctx); n > 0 {
				return s.store.Threads(ctx, projectID)
			}
			break
		}
	}
	return list, nil
}

// Thread implements ports.ProductOwner.
func (s *Service) Thread(ctx context.Context, threadID, before string) (*ports.ProductOwnerThreadPage, error) {
	t, err := s.store.Thread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if t.PendingHeldActions > 0 {
		if n, _ := s.ExpireStale(ctx); n > 0 {
			t, _ = s.store.Thread(ctx, threadID)
		}
	}
	items, more, err := s.store.Items(ctx, threadID, before, domain.ProductOwnerThreadPage)
	if err != nil {
		return nil, err
	}
	return &ports.ProductOwnerThreadPage{Thread: *t, Items: items, HasEarlier: more}, nil
}

// checkMessage is INVALID_INPUT for a message the thread does not take.
func checkMessage(message string, attachmentIDs []string) (string, error) {
	message = strings.TrimSpace(message)
	switch {
	case len(attachmentIDs) > domain.ProductOwnerMaxAttachments:
		return "", invalidInput(fmt.Sprintf("a message takes at most %d attachments", domain.ProductOwnerMaxAttachments))
	case len(attachmentIDs) > 0:
		return "", invalidInput("attachments are not available yet: " + attachmentIDs[0] + " is not an attachment of this project")
	case message == "":
		return "", invalidInput("message is empty")
	case len([]rune(message)) > domain.ProductOwnerMaxMessage:
		return "", invalidInput(fmt.Sprintf("message is over %d characters", domain.ProductOwnerMaxMessage))
	}
	return message, nil
}

// StartThread implements ports.ProductOwner.
func (s *Service) StartThread(ctx context.Context, projectID, message string, attachmentIDs []string) (string, string, error) {
	if err := s.available(); err != nil {
		return "", "", err
	}
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return "", "", err
	}
	st, err := s.store.Settings(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	if !st.Enabled {
		return "", "", offError()
	}
	message, err = checkMessage(message, attachmentIDs)
	if err != nil {
		return "", "", err
	}
	spec := ports.ProductOwnerSessionSpec{ProjectID: projectID, AgentID: st.AgentID, Prompt: st.Prompt}
	sessionID, err := s.sessions.StartProductOwnerSession(ctx, spec)
	if err != nil {
		return "", "", unavailableOr(err)
	}
	now := time.Now()
	id, err := newID()
	if err != nil {
		return "", "", err
	}
	t := domain.ProductOwnerThread{ID: id, ProjectID: projectID, SessionID: sessionID, Title: domain.ProductOwnerThreadTitle(message),
		Turn: domain.ProductOwnerTurnIdle, StartedAt: now, LastActivityAt: now, AgentID: st.AgentID, Prompt: st.Prompt,
		LastMessageExcerpt: domain.ProductOwnerExcerpt(message)}
	if err := s.store.CreateThread(ctx, t); err != nil {
		_ = s.sessions.Stop(ctx, sessionID)
		return "", "", err
	}
	lock := s.threadLock(t.ID)
	lock.Lock()
	defer lock.Unlock()
	r := s.attach(t)
	itemID, err := s.deliver(ctx, &t, r, message)
	if err != nil {
		s.dropRun(r)
		_ = s.sessions.Stop(ctx, sessionID)
		return "", "", err
	}
	return t.ID, itemID, nil
}

// SendMessage implements ports.ProductOwner.
func (s *Service) SendMessage(ctx context.Context, threadID, message string, attachmentIDs []string) (string, error) {
	if err := s.available(); err != nil {
		return "", err
	}
	t, err := s.store.Thread(ctx, threadID)
	if err != nil {
		return "", err
	}
	st, err := s.store.Settings(ctx, t.ProjectID)
	if err != nil {
		return "", err
	}
	if !st.Enabled {
		return "", offError()
	}
	message, err = checkMessage(message, attachmentIDs)
	if err != nil {
		return "", err
	}
	// Refused at once, before waiting on the lock a wake-up holds.
	if s.busy(t) {
		return "", busyError()
	}
	lock := s.threadLock(t.ID)
	lock.Lock()
	defer lock.Unlock()
	// Read again under the lock: a turn may have started since.
	if t, err = s.store.Thread(ctx, threadID); err != nil {
		return "", err
	}
	if s.busy(t) {
		return "", busyError()
	}
	r := s.runOf(t.ID)
	if r != nil && s.sessions.ProductOwnerRunning(t.SessionID) {
		return s.deliver(ctx, t, r, message)
	}
	// The person's message is stored first and kept whatever happens next; the
	// thread reads resuming while its session wakes up.
	s.setWaking(t.ID, true)
	defer s.setWaking(t.ID, false)
	item, err := s.storeMessage(ctx, t, message)
	if err != nil {
		return "", err
	}
	if _, err := s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool { th.Turn = domain.ProductOwnerTurnResuming; return true }); err != nil {
		return "", err
	}
	s.announceThread(*t)
	spec := ports.ProductOwnerSessionSpec{ProjectID: t.ProjectID, AgentID: t.AgentID, Prompt: t.Prompt}
	if err := s.sessions.ResumeProductOwnerSession(ctx, t.SessionID, spec); err != nil {
		s.fail(ctx, t, item.ID, "SESSION_RESUME_FAILED", "The product owner's session could not be resumed: "+err.Error())
		if wrapped := unavailableOr(err); wrapped != err {
			return "", wrapped
		}
		return item.ID, nil
	}
	r = s.attach(*t)
	if err := s.send(ctx, t, r, item.ID, message); err != nil {
		return "", err
	}
	return item.ID, nil
}

// busy says a turn of the thread is under way: it reads running, resuming or
// interrupted with a process, a wake-up or the boot's recovery behind it. A turn
// state with nothing behind it is stale (a crash left it) and never keeps the
// thread from taking a message.
func (s *Service) busy(t *domain.ProductOwnerThread) bool {
	if t.Turn == domain.ProductOwnerTurnIdle {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[t.ID] != nil || s.recovering[t.ID] || s.waking[t.ID]
}

func (s *Service) setWaking(threadID string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if on {
		s.waking[threadID] = true
		return
	}
	delete(s.waking, threadID)
}

func busyError() error {
	return &domain.StructuredError{Code: "THREAD_BUSY", Message: "THREAD_BUSY: a turn of this thread is running; wait for the answer or stop it"}
}

// storeMessage stores the person's message as the thread's next item.
func (s *Service) storeMessage(ctx context.Context, t *domain.ProductOwnerThread, message string) (domain.ProductOwnerItem, error) {
	item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
		Author: domain.ProductOwnerAuthorPerson, Text: message})
	if err != nil {
		return item, err
	}
	if _, err := s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool {
		th.LastMessageExcerpt, th.LastActivityAt = domain.ProductOwnerExcerpt(message), item.At
		return true
	}); err != nil {
		return item, err
	}
	s.announceItem(t, item)
	return item, nil
}

// fail ends a turn that could not begin: a failure item names what happened
// and which message to send again, and the thread is idle. Nothing else
// changes.
func (s *Service) fail(ctx context.Context, t *domain.ProductOwnerThread, retryItemID, code, text string) {
	item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemFailure, At: time.Now(),
		Code: code, Text: text, RetryItemID: retryItemID})
	if err == nil {
		s.announceItem(t, item)
	}
	if _, err := s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool {
		th.Turn, th.LastActivityAt = domain.ProductOwnerTurnIdle, time.Now()
		return true
	}); err == nil {
		s.announceThread(*t)
	}
}

// deliver stores the person's message as the thread's next item, marks the
// turn running and hands the message to the thread's process.
func (s *Service) deliver(ctx context.Context, t *domain.ProductOwnerThread, r *run, message string) (string, error) {
	item, err := s.storeMessage(ctx, t, message)
	if err != nil {
		return "", err
	}
	if err := s.send(ctx, t, r, item.ID, message); err != nil {
		return "", err
	}
	return item.ID, nil
}

// send marks the turn running and hands the stored message to the thread's
// process. A process that cannot take it fails the turn.
func (s *Service) send(ctx context.Context, t *domain.ProductOwnerThread, r *run, itemID, message string) error {
	if _, err := s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool { th.Turn = domain.ProductOwnerTurnRunning; return true }); err != nil {
		return err
	}
	s.announceThread(*t)
	s.resetIdle(r, false)
	// The answers to held actions since its last turn go in front of the message.
	lines, told := s.heldAnswers(ctx, t.ID)
	input, seen, read := s.withMemory(ctx, t, lines+message)
	if err := s.sessions.Send(ctx, t.SessionID, input); err != nil {
		s.fail(ctx, t, itemID, "SESSION_SEND_FAILED", "The message did not reach the product owner's session: "+err.Error())
		return nil
	}
	if len(told) > 0 {
		s.markTold(ctx, told)
	}
	if read {
		s.sawMemory(ctx, t, seen)
	}
	return nil
}

// RenameThread implements ports.ProductOwner.
func (s *Service) RenameThread(ctx context.Context, threadID, title string) (*domain.ProductOwnerThread, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > domain.ProductOwnerMaxTitle {
		if _, err := s.store.Thread(ctx, threadID); err != nil {
			return nil, err
		}
		return nil, invalidInput("title must be 1 to 200 characters")
	}
	t, err := s.update(ctx, threadID, nil, func(th *domain.ProductOwnerThread) bool {
		th.Title, th.TitleSetByPerson = title, true
		return true
	})
	if err != nil {
		return nil, err
	}
	s.announceThread(*t)
	return t, nil
}

// StopTurn implements ports.ProductOwner.
func (s *Service) StopTurn(ctx context.Context, threadID string) error {
	if err := s.available(); err != nil {
		return err
	}
	if _, err := s.store.Thread(ctx, threadID); err != nil {
		return err
	}
	lock := s.threadLock(threadID)
	lock.Lock()
	defer lock.Unlock()
	// Read under the lock: a wake-up or a recovery may have moved the turn.
	t, err := s.store.Thread(ctx, threadID)
	if err != nil {
		return err
	}
	if r := s.runOf(t.ID); r != nil {
		r.mu.Lock()
		r.retired = true
		if r.cur != nil {
			r.cur.Text, r.cur.Streaming, r.cur.Stopped = r.text.String(), false, true
			_ = s.store.SaveItem(ctx, *r.cur)
			s.announceItem(t, *r.cur)
			r.cur = nil
		}
		r.mu.Unlock()
		s.dropRun(r)
		_ = s.sessions.Stop(ctx, t.SessionID)
		s.waitProcessEnd(ctx, t.SessionID)
	}
	// A stop that came before the first word, or while the session was waking
	// or queued for recovery, still answers the person's message with an empty
	// stopped one, so that nothing later reads it as unanswered. A message
	// already answered is left alone.
	if s.unanswered(ctx, t.ID) {
		if item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
			Author: domain.ProductOwnerAuthorProductOwner, Stopped: true}); err == nil {
			s.announceItem(t, item)
		}
	}
	s.finishTurn(t.ID)
	return nil
}

// waitProcessEnd waits for a stopped process to be gone, so that the next
// message resumes the conversation instead of meeting the old process. The
// runtime kills a process that outlives its stop grace, so the wait is short.
func (s *Service) waitProcessEnd(ctx context.Context, sessionID string) {
	deadline := time.Now().Add(7 * time.Second)
	for s.sessions.ProductOwnerRunning(sessionID) && time.Now().Before(deadline) && ctx.Err() == nil {
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *Service) runOf(threadID string) *run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[threadID]
}

func (s *Service) dropRun(r *run) {
	s.mu.Lock()
	if s.runs[r.threadID] == r {
		delete(s.runs, r.threadID)
	}
	s.mu.Unlock()
	r.stopIdle()
	r.unsub()
}

// attach follows the process of a thread's session: from now on its events
// become the thread's items.
func (s *Service) attach(t domain.ProductOwnerThread) *run {
	ch, _, unsub := s.sessions.Subscribe(t.SessionID)
	r := &run{threadID: t.ID, projectID: t.ProjectID, sessionID: t.SessionID, unsub: unsub}
	s.mu.Lock()
	old := s.runs[t.ID]
	s.runs[t.ID] = r
	s.mu.Unlock()
	if old != nil {
		// A process that is on its way out: what it still says is not the new
		// turn's.
		old.mu.Lock()
		old.retired = true
		old.mu.Unlock()
		old.stopIdle()
		old.unsub()
	}
	go s.consume(r, ch)
	return r
}

// resetIdle restarts the wait for the next message. A thread whose turn is
// running does not end; the wait restarts when the turn does.
func (s *Service) resetIdle(r *run, armed bool) {
	s.mu.Lock()
	d := s.idle
	s.mu.Unlock()
	r.idleMu.Lock()
	defer r.idleMu.Unlock()
	if r.idle != nil {
		r.idle.Stop()
		r.idle = nil
	}
	if !armed || d <= 0 {
		return
	}
	r.idle = time.AfterFunc(d, func() { s.stopIdle(r) })
}

// stopIdle ends the process of a thread that has waited for its next message.
// It takes the thread's lock and checks the turn, so a message that arrives
// now is either delivered to the process before this, which then leaves it
// alone, or finds no run after it and wakes the session with a fresh resume.
func (s *Service) stopIdle(r *run) {
	lock := s.threadLock(r.threadID)
	lock.Lock()
	defer lock.Unlock()
	ctx := context.Background()
	t, err := s.store.Thread(ctx, r.threadID)
	if err != nil || t.Turn != domain.ProductOwnerTurnIdle || s.runOf(r.threadID) != r {
		return
	}
	r.mu.Lock()
	r.retired = true
	r.mu.Unlock()
	s.dropRun(r)
	_ = s.sessions.Stop(ctx, r.sessionID)
	s.waitProcessEnd(ctx, r.sessionID)
}

func (r *run) stopIdle() {
	r.idleMu.Lock()
	defer r.idleMu.Unlock()
	if r.idle != nil {
		r.idle.Stop()
		r.idle = nil
	}
}

// consume turns a process's events into the thread's items, until the
// process ends.
func (s *Service) consume(r *run, ch <-chan ports.SessionEvent) {
	for ev := range ch {
		s.handle(r, ev)
	}
	s.ended(r)
}

func (s *Service) handle(r *run, ev ports.SessionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired {
		return
	}
	ctx := context.Background()
	t, err := s.store.Thread(ctx, r.threadID)
	if err != nil {
		return
	}
	switch ev.Type {
	case "output_delta":
		if ev.DeltaKind != "text" || ev.Text == "" {
			return
		}
		if r.cur == nil {
			item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
				Author: domain.ProductOwnerAuthorProductOwner, Streaming: true})
			if err != nil {
				return
			}
			r.cur = &item
			r.text.Reset()
			s.announceItem(t, item)
		}
		r.text.WriteString(ev.Text)
		s.announce(ports.ProjectChange{ProductOwnerDelta: &ports.ProductOwnerDeltaChange{ProjectID: t.ProjectID, ThreadID: t.ID, ItemID: r.cur.ID, Text: ev.Text}})
		if time.Since(r.lastSaved) > 250*time.Millisecond {
			// What has arrived is kept as it comes, so a cut-off turn leaves its
			// partial answer in the thread.
			r.cur.Text = r.text.String()
			_ = s.store.SaveItem(ctx, *r.cur)
			r.lastSaved = time.Now()
		}
	case "output":
		if r.cur != nil {
			r.cur.Text, r.cur.Streaming = ev.Text, false
			_ = s.store.SaveItem(ctx, *r.cur)
			s.setExcerpt(ctx, t.ID, ev.Text)
			s.announceItem(t, *r.cur)
			r.cur = nil
		} else if strings.TrimSpace(ev.Text) != "" {
			item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
				Author: domain.ProductOwnerAuthorProductOwner, Text: ev.Text})
			if err == nil {
				s.setExcerpt(ctx, t.ID, ev.Text)
				s.announceItem(t, item)
			}
		}
	case "tool_use":
		if text := activityText(ev.ToolName, ev.ToolInput); text != "" {
			item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemActivity, At: time.Now(), Text: text})
			if err == nil {
				s.announceItem(t, item)
			}
		}
	case "usage":
		if r.cur != nil {
			r.cur.Text, r.cur.Streaming = r.text.String(), false
			_ = s.store.SaveItem(ctx, *r.cur)
			s.setExcerpt(ctx, t.ID, r.cur.Text)
			s.announceItem(t, *r.cur)
			r.cur = nil
		}
		s.finishTurn(t.ID)
		s.resetIdle(r, true)
	}
}

// ended is the process's end: the turn it was in is over, whatever way it
// went, unless the server itself is stopping, which a restart resumes.
func (s *Service) ended(r *run) {
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	r.stopIdle()
	if closing {
		return
	}
	// A run another has replaced, or that was dropped, is not this thread's
	// process any more: its end changes nothing.
	s.mu.Lock()
	current := s.runs[r.threadID] == r
	s.mu.Unlock()
	if !current {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired {
		return
	}
	ctx := context.Background()
	t, err := s.store.Thread(ctx, r.threadID)
	if err != nil {
		return
	}
	cut := r.cur != nil || t.Turn == domain.ProductOwnerTurnRunning
	if r.cur != nil {
		// The process ended in the middle of an answer: what arrived is kept.
		r.cur.Text, r.cur.Streaming = r.text.String(), false
		_ = s.store.SaveItem(ctx, *r.cur)
		s.announceItem(t, *r.cur)
		r.cur = nil
	}
	s.mu.Lock()
	if s.runs[r.threadID] == r {
		delete(s.runs, r.threadID)
	}
	s.mu.Unlock()
	if cut {
		s.fail(ctx, t, s.lastPersonMessage(ctx, t.ID), "SESSION_ENDED", "The product owner's session ended before it finished its answer.")
		return
	}
	s.finishTurn(r.threadID)
}

// lastPersonMessage is the id of the person's newest message in the thread, ""
// when there is none.
func (s *Service) lastPersonMessage(ctx context.Context, threadID string) string {
	items, _, err := s.store.Items(ctx, threadID, "", recentItems)
	if err != nil {
		return ""
	}
	for i := len(items) - 1; i >= 0; i-- {
		if it := items[i]; it.Kind == domain.ProductOwnerItemMessage && it.Author == domain.ProductOwnerAuthorPerson {
			return it.ID
		}
	}
	return ""
}

// recentItems is how many of a thread's newest items the service looks through
// for the last of a kind.
const recentItems = 20

// setExcerpt keeps the first line of the thread's last message.
func (s *Service) setExcerpt(ctx context.Context, threadID, text string) {
	ex := domain.ProductOwnerExcerpt(text)
	_, _ = s.update(ctx, threadID, nil, func(th *domain.ProductOwnerThread) bool {
		if ex == "" || ex == th.LastMessageExcerpt {
			return false
		}
		th.LastMessageExcerpt = ex
		return true
	})
}

// finishTurn marks the thread idle.
func (s *Service) finishTurn(threadID string) {
	changed := false
	t, err := s.update(context.Background(), threadID, nil, func(th *domain.ProductOwnerThread) bool {
		if th.Turn == domain.ProductOwnerTurnIdle {
			return false
		}
		th.Turn, th.LastActivityAt = domain.ProductOwnerTurnIdle, time.Now()
		changed = true
		return true
	})
	if err == nil && changed {
		s.announceThread(*t)
	}
}

// Recover resumes the turns a restart cut off (ADR 0080, after ADR 0078's
// boot handling): each thread that was mid-turn reads interrupted until its
// session is resumed, then continues with a notice in the thread and a turn
// that tells claude what happened. A thread whose resume fails stays
// interrupted, for the next boot. The product owner being switched off does not
// stop a turn that was already running.
//
// Which threads to recover is decided here, before the server takes a request:
// each is marked interrupted and counts as busy, so a message cannot meet a
// thread half way through its recovery. The resumes then run behind the boot,
// each under its thread's lock.
func (s *Service) Recover(ctx context.Context) {
	if cut := s.decideRecovery(ctx); len(cut) > 0 {
		go s.resumeAll(ctx, cut)
	}
}

func (s *Service) decideRecovery(ctx context.Context) map[string]domain.ProductOwnerThread {
	cut := map[string]domain.ProductOwnerThread{}
	for _, turn := range []string{domain.ProductOwnerTurnRunning, domain.ProductOwnerTurnResuming, domain.ProductOwnerTurnInterrupted} {
		threads, err := s.store.ThreadsWithTurn(ctx, turn)
		if err != nil {
			slog.Warn("read the product owner threads a restart cut off", "err", err)
			continue
		}
		for _, t := range threads {
			cut[t.ID] = t
		}
	}
	// A thread idle with the person's message unanswered was cut off between
	// storing the message and starting the turn.
	if idle, err := s.store.ThreadsWithTurn(ctx, domain.ProductOwnerTurnIdle); err == nil {
		for _, t := range idle {
			if s.unanswered(ctx, t.ID) {
				cut[t.ID] = t
			}
		}
	}
	s.mu.Lock()
	for id := range cut {
		s.recovering[id] = true
	}
	s.mu.Unlock()
	for id, t := range cut {
		cur, err := s.update(ctx, id, nil, func(th *domain.ProductOwnerThread) bool {
			th.Turn = domain.ProductOwnerTurnInterrupted
			return true
		})
		if err != nil {
			slog.Warn("mark a product owner thread interrupted", "thread", id, "err", err)
			continue
		}
		s.announceThread(*cur)
		t.Turn = domain.ProductOwnerTurnInterrupted
		cut[id] = t
	}
	return cut
}

func (s *Service) resumeAll(ctx context.Context, cut map[string]domain.ProductOwnerThread) {
	for id, t := range cut {
		if err := s.resumeInterrupted(ctx, t); err != nil {
			slog.Warn("resume a product owner thread a restart cut off", "thread", id, "err", err)
		}
		s.mu.Lock()
		delete(s.recovering, id)
		s.mu.Unlock()
	}
}

// unanswered says the newest message or failure in the thread is the person's
// message: nothing answered it.
func (s *Service) unanswered(ctx context.Context, threadID string) bool {
	items, _, err := s.store.Items(ctx, threadID, "", recentItems)
	if err != nil {
		return false
	}
	for i := len(items) - 1; i >= 0; i-- {
		switch it := items[i]; {
		case it.Kind == domain.ProductOwnerItemMessage:
			return it.Author == domain.ProductOwnerAuthorPerson
		case it.Kind == domain.ProductOwnerItemFailure:
			return false
		}
	}
	return false
}

// resumeInterrupted wakes a cut-off thread's session and continues the turn.
// A session that cannot be woken fails the turn: the person's message is kept,
// the thread goes idle, and sending again is the retry.
func (s *Service) resumeInterrupted(ctx context.Context, t domain.ProductOwnerThread) error {
	if s.sessions == nil {
		return nil
	}
	lock := s.threadLock(t.ID)
	lock.Lock()
	defer lock.Unlock()
	// Read again under the lock: only a thread still interrupted is resumed.
	cur, err := s.store.Thread(ctx, t.ID)
	if err != nil || cur.Turn != domain.ProductOwnerTurnInterrupted {
		return err
	}
	t = *cur
	spec := ports.ProductOwnerSessionSpec{ProjectID: t.ProjectID, AgentID: t.AgentID, Prompt: t.Prompt}
	if err := s.sessions.ResumeProductOwnerSession(ctx, t.SessionID, spec); err != nil {
		s.fail(ctx, &t, s.lastPersonMessage(ctx, t.ID), "SESSION_RESUME_FAILED", "The product owner's session could not be resumed after the restart: "+err.Error())
		return err
	}
	s.finishPartialAnswers(ctx, &t)
	r := s.attach(t)
	notice, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemNotice, At: time.Now(), Text: "Continued after a restart"})
	if err != nil {
		return err
	}
	if _, err := s.update(ctx, t.ID, &t, func(th *domain.ProductOwnerThread) bool {
		th.Turn, th.LastActivityAt = domain.ProductOwnerTurnRunning, notice.At
		return true
	}); err != nil {
		return err
	}
	s.announceItem(&t, notice)
	s.announceThread(t)
	s.resetIdle(r, false)
	if err := s.sessions.Send(ctx, t.SessionID, s.continueTurn(ctx, t)); err != nil {
		s.fail(ctx, &t, s.lastPersonMessage(ctx, t.ID), "SESSION_SEND_FAILED", "The product owner's session did not take the turn after the restart: "+err.Error())
		return err
	}
	return nil
}

// finishPartialAnswers ends the answers the cut left streaming: what arrived
// of each is kept, marked stopped.
func (s *Service) finishPartialAnswers(ctx context.Context, t *domain.ProductOwnerThread) {
	items, _, err := s.store.Items(ctx, t.ID, "", recentItems)
	if err != nil {
		return
	}
	for _, it := range items {
		if it.Kind == domain.ProductOwnerItemMessage && it.Streaming {
			it.Streaming = false
			if err := s.store.SaveItem(ctx, it); err == nil {
				s.announceItem(t, it)
			}
		}
	}
}

// continueTurn is the turn that wakes a resumed thread: what happened, and
// the person's last message, so that a message the cut came before claude read
// it is answered too.
func (s *Service) continueTurn(ctx context.Context, t domain.ProductOwnerThread) string {
	text := "The harness restarted during your turn, so it was interrupted. Before you redo anything, check what you already said in this conversation, then continue where you were."
	items, _, err := s.store.Items(ctx, t.ID, "", recentItems)
	if err != nil {
		return text
	}
	for i := len(items) - 1; i >= 0; i-- {
		if it := items[i]; it.Kind == domain.ProductOwnerItemMessage && it.Author == domain.ProductOwnerAuthorPerson {
			return text + "\n\nThe person's last message was:\n\n" + it.Text + "\n\nIf you have not answered it in full, answer it now."
		}
	}
	return text
}

// Close tells the service the server is stopping: the processes that end with
// it leave their threads as they are, for the next boot to resume.
func (s *Service) Close() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
}

func (s *Service) announceItem(t *domain.ProductOwnerThread, item domain.ProductOwnerItem) {
	s.announce(ports.ProjectChange{ProductOwnerThreadItem: &ports.ProductOwnerThreadItemChange{ProjectID: t.ProjectID, ThreadID: t.ID, Item: item}})
}

func (s *Service) announceThread(t domain.ProductOwnerThread) {
	s.announce(ports.ProjectChange{ProductOwnerThread: &ports.ProductOwnerThreadChange{ProjectID: t.ProjectID, Thread: t}})
}

func offError() error {
	return &domain.StructuredError{Code: "PRODUCT_OWNER_OFF", Message: "PRODUCT_OWNER_OFF: the product owner of this project is switched off"}
}

// unavailableCodes are the refusals of a start that mean this machine cannot
// run claude yet.
var unavailableCodes = map[string]bool{
	"CLAUDE_CLI_NOT_FOUND": true, "AGENT_CLI_NOT_FOUND": true, "AGENT_CREDENTIAL_MISSING": true,
	"MANAGED_CLAUDE_MISSING": true, "MANAGED_CLAUDE_INSTALLING": true, "MANAGED_CLAUDE_INSTALL_FAILED": true,
	"SANDBOX_DRIVER_MISSING": true,
}

// unavailableOr is PRODUCT_OWNER_UNAVAILABLE, with where to fix it, for a
// start that failed because claude cannot run; any other error as it is.
func unavailableOr(err error) error {
	var se *domain.StructuredError
	if errors.As(err, &se) && unavailableCodes[se.Code] {
		return domain.NewCatalogError("PRODUCT_OWNER_UNAVAILABLE", "PRODUCT_OWNER_UNAVAILABLE: "+se.Message, map[string]any{"fix_url": agentready.Page})
	}
	return err
}

// activityText is the one-line note a tool use leaves in the thread.
func activityText(tool string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := in[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch tool {
	case "Read", "NotebookRead":
		if p := str("file_path", "path"); p != "" {
			return "Reading " + p
		}
	case "Grep", "Glob":
		if p := str("pattern"); p != "" {
			return "Searching for " + p
		}
	case "Bash":
		if c := str("command"); c != "" {
			c, _, _ = strings.Cut(c, "\n")
			if len(c) > 80 {
				c = c[:80] + "…"
			}
			return "Running " + c
		}
	}
	return "Using " + strings.TrimPrefix(tool, "mcp__task__")
}
