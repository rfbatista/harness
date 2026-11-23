package productowner

import (
	"context"
	"encoding/json"
	"errors"
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
	return s.store.Threads(ctx, projectID)
}

// Thread implements ports.ProductOwner.
func (s *Service) Thread(ctx context.Context, threadID, before string) (*ports.ProductOwnerThreadPage, error) {
	t, err := s.store.Thread(ctx, threadID)
	if err != nil {
		return nil, err
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
		return "", invalidInput("a message takes at most 5 attachments")
	case len(attachmentIDs) > 0:
		return "", invalidInput("attachments are not available yet: " + attachmentIDs[0] + " is not an attachment of this project")
	case message == "":
		return "", invalidInput("message is empty")
	case len([]rune(message)) > domain.ProductOwnerMaxMessage:
		return "", invalidInput("message is over 20,000 characters")
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
		Turn: domain.ProductOwnerTurnIdle, StartedAt: now, LastActivityAt: now, AgentID: st.AgentID, Prompt: st.Prompt}
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
	lock := s.threadLock(t.ID)
	lock.Lock()
	defer lock.Unlock()
	// Read again under the lock: a turn may have started since.
	if t, err = s.store.Thread(ctx, threadID); err != nil {
		return "", err
	}
	if t.Turn != domain.ProductOwnerTurnIdle {
		return "", &domain.StructuredError{Code: "THREAD_BUSY", Message: "THREAD_BUSY: a turn of this thread is running; wait for the answer or stop it"}
	}
	r := s.runOf(t.ID)
	if r == nil || !s.sessions.ProductOwnerRunning(t.SessionID) {
		spec := ports.ProductOwnerSessionSpec{ProjectID: t.ProjectID, AgentID: t.AgentID, Prompt: t.Prompt}
		if err := s.sessions.ResumeProductOwnerSession(ctx, t.SessionID, spec); err != nil {
			return "", unavailableOr(err)
		}
		r = s.attach(*t)
	}
	return s.deliver(ctx, t, r, message)
}

// deliver stores the person's message as the thread's next item, marks the
// turn running and hands the message to the thread's process.
func (s *Service) deliver(ctx context.Context, t *domain.ProductOwnerThread, r *run, message string) (string, error) {
	now := time.Now()
	item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemMessage, At: now,
		Author: domain.ProductOwnerAuthorPerson, Text: message})
	if err != nil {
		return "", err
	}
	t.Turn, t.LastActivityAt = domain.ProductOwnerTurnRunning, now
	if err := s.store.SaveThread(ctx, *t); err != nil {
		return "", err
	}
	s.announceItem(t, item)
	s.announceThread(*t)
	s.resetIdle(r, false)
	if err := s.sessions.Send(ctx, t.SessionID, message); err != nil {
		s.finishTurn(t.ID)
		return "", err
	}
	return item.ID, nil
}

// StopTurn implements ports.ProductOwner.
func (s *Service) StopTurn(ctx context.Context, threadID string) error {
	if err := s.available(); err != nil {
		return err
	}
	t, err := s.store.Thread(ctx, threadID)
	if err != nil {
		return err
	}
	lock := s.threadLock(t.ID)
	lock.Lock()
	defer lock.Unlock()
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
	}
	s.finishTurn(t.ID)
	return nil
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
	if r.idle != nil {
		r.idle.Stop()
	}
	r.unsub()
}

// attach follows the process of a thread's session: from now on its events
// become the thread's items.
func (s *Service) attach(t domain.ProductOwnerThread) *run {
	ch, _, unsub := s.sessions.Subscribe(t.SessionID)
	r := &run{threadID: t.ID, projectID: t.ProjectID, sessionID: t.SessionID, unsub: unsub}
	s.mu.Lock()
	s.runs[t.ID] = r
	s.mu.Unlock()
	go s.consume(r, ch)
	return r
}

// resetIdle restarts the wait for the next message. A thread whose turn is
// running does not end; the wait restarts when the turn does.
func (s *Service) resetIdle(r *run, armed bool) {
	s.mu.Lock()
	d := s.idle
	s.mu.Unlock()
	if r.idle != nil {
		r.idle.Stop()
		r.idle = nil
	}
	if !armed || d <= 0 {
		return
	}
	r.idle = time.AfterFunc(d, func() {
		if s.runOf(r.threadID) != r {
			return
		}
		_ = s.sessions.Stop(context.Background(), r.sessionID)
	})
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
			s.announceItem(t, *r.cur)
			r.cur = nil
		} else if strings.TrimSpace(ev.Text) != "" {
			item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
				Author: domain.ProductOwnerAuthorProductOwner, Text: ev.Text})
			if err == nil {
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
	if r.idle != nil {
		r.idle.Stop()
	}
	if closing {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired {
		return
	}
	if r.cur != nil {
		r.cur.Text, r.cur.Streaming, r.cur.Stopped = r.text.String(), false, true
		_ = s.store.SaveItem(context.Background(), *r.cur)
		if t, err := s.store.Thread(context.Background(), r.threadID); err == nil {
			s.announceItem(t, *r.cur)
		}
		r.cur = nil
	}
	s.mu.Lock()
	if s.runs[r.threadID] == r {
		delete(s.runs, r.threadID)
	}
	s.mu.Unlock()
	s.finishTurn(r.threadID)
}

// finishTurn marks the thread idle.
func (s *Service) finishTurn(threadID string) {
	ctx := context.Background()
	t, err := s.store.Thread(ctx, threadID)
	if err != nil || t.Turn == domain.ProductOwnerTurnIdle {
		return
	}
	t.Turn, t.LastActivityAt = domain.ProductOwnerTurnIdle, time.Now()
	if err := s.store.SaveThread(ctx, *t); err != nil {
		return
	}
	s.announceThread(*t)
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
