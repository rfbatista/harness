package taskchannel_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// delivery is a fake courier. A session in busy queues turns by key until
// endTurn; any other running session takes them at once.
type delivery struct {
	mu      sync.Mutex
	busy    map[string]bool
	queued  map[string][]queuedTurn
	turns   map[string][]string
	stopped map[string]bool
	now     func() time.Time
}

type queuedTurn struct {
	key, text string
	done      func(time.Time)
}

func newDelivery(now func() time.Time) *delivery {
	return &delivery{busy: map[string]bool{}, queued: map[string][]queuedTurn{}, turns: map[string][]string{}, stopped: map[string]bool{}, now: now}
}

func (d *delivery) Deliver(id, key, text string, done func(time.Time)) (bool, error) {
	d.mu.Lock()
	if d.stopped[id] {
		d.mu.Unlock()
		return false, &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "not running"}
	}
	if d.busy[id] {
		q := d.queued[id]
		for i := range q {
			if q[i].key == key {
				q[i] = queuedTurn{key, text, done}
				d.mu.Unlock()
				return false, nil
			}
		}
		d.queued[id] = append(q, queuedTurn{key, text, done})
		d.mu.Unlock()
		return false, nil
	}
	d.turns[id] = append(d.turns[id], text)
	d.mu.Unlock()
	if done != nil {
		done(d.now())
	}
	return true, nil
}

func (d *delivery) endTurn(id string) {
	d.mu.Lock()
	q := d.queued[id]
	delete(d.queued, id)
	d.busy[id] = false
	for _, t := range q {
		d.turns[id] = append(d.turns[id], t.text)
	}
	d.mu.Unlock()
	for _, t := range q {
		if t.done != nil {
			t.done(d.now())
		}
	}
}

func (d *delivery) got(id string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.turns[id]...)
}

type feed struct {
	mu      sync.Mutex
	changes []ports.ProjectChange
}

func (f *feed) AnnounceChange(c ports.ProjectChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, c)
}

func (f *feed) count(pred func(ports.ProjectChange) bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.changes {
		if pred(c) {
			n++
		}
	}
	return n
}

type streams struct {
	mu  sync.Mutex
	got map[string][]string
}

func (s *streams) Announce(id string, ev ports.SessionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got[id] = append(s.got[id], ev.Type)
}

type fixture struct {
	svc      *taskchannel.Service
	plan     *planning.Service
	sessions *sqlite.SessionRepository
	del      *delivery
	feed     *feed
	streams  *streams
	bus      *eventbus.Bus
	project  string
	ticket   string
	clock    time.Time
}

func (f *fixture) now() time.Time          { return f.clock }
func (f *fixture) advance(d time.Duration) { f.clock = f.clock.Add(d) }

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	f := &fixture{plan: plan, sessions: sqlite.NewSessionRepository(db), feed: &feed{}, streams: &streams{got: map[string][]string{}},
		bus: eventbus.New(), project: p.ID, clock: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)}
	tk, _ := plan.CreateTicket(context.Background(), p.ID, "Architect highlights", "", domain.TicketStatusTodo)
	f.ticket = tk.ID
	f.del = newDelivery(f.now)
	f.svc = taskchannel.New(f.sessions, plan, taskchannel.Repositories{
		Messages: sqlite.NewTaskMessageRepository(db), Reviews: sqlite.NewReviewRequestRepository(db), Checks: sqlite.NewStatusCheckRepository(db),
	})
	f.svc.SetClock(f.now)
	f.svc.Delivery, f.svc.Feed, f.svc.Announcer = f.del, f.feed, f.streams
	f.svc.Subscribe(f.bus)
	plan.StatusHistory, plan.Events, plan.Decorator = sqlite.NewTaskStatusChangeRepository(db), f.bus, f.svc
	return f
}

// start records a session on the task and announces it, as the
// orchestration does.
func (f *fixture) start(t *testing.T, id, parent string, mode domain.SessionMode, minutes *int) *domain.Session {
	t.Helper()
	s, err := f.sessions.Create(&domain.Session{ID: id, ProjectID: f.project, TicketID: f.ticket, ParentSessionID: parent, Mode: mode, Status: domain.SessionRunning, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // created_at orders architects
	if err := f.bus.Publish(context.Background(), domain.SessionStarted{SessionID: id, ProjectID: f.project, TicketID: f.ticket, ParentSessionID: parent, Mode: mode, StatusCheckMinutes: minutes}); err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fixture) end(t *testing.T, id string, status domain.SessionStatus) {
	t.Helper()
	_ = f.sessions.UpdateStatus(id, status)
	_ = f.bus.Publish(context.Background(), domain.SessionEnded{SessionID: id, Status: status})
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if errs.Code(err) != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func intp(n int) *int { return &n }

var ctx = context.Background()

func TestNoArchitect_BehavesAsToday(t *testing.T) {
	f := newFixture(t)
	f.start(t, "peer", "", "", nil)
	_, err := f.svc.SendToArchitect(ctx, "peer", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: "anyone?"})
	wantCode(t, err, "NO_ARCHITECT")
	tk, err := f.svc.SetTaskStatus(ctx, "peer", domain.TicketStatusInProgress, "")
	if err != nil || tk.Status != domain.TicketStatusInProgress {
		t.Fatalf("a peer on a task with no architect moves it: %+v %v", tk, err)
	}
	if role, _ := f.svc.SessionRole(ctx, "peer"); role != domain.RolePeer {
		t.Fatalf("role %q", role)
	}
	got, _ := f.plan.GetTicket(ctx, f.ticket)
	if got.ArchitectSessionID != nil || got.PendingReviews != 0 {
		t.Fatalf("ticket derived fields: %+v", got)
	}
}

func TestMessages_DeliveredNowOrAtTurnEnd(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)

	m, err := f.svc.SendToArchitect(ctx, "dev", ports.TaskMessageInput{Kind: domain.MessageStatusReport, Status: domain.ReportWorking, Body: "halfway", Subject: "progress"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Delivered || m.DeliveredAt == nil || m.ToSessionID != "arch" || m.Queued {
		t.Fatalf("an idle architect takes it at once: %+v", m)
	}
	turn := f.del.got("arch")[0]
	if !strings.HasPrefix(turn, "[task message "+m.ID+" · status_report/working · from claude session dev]\nSubject: progress\nhalfway") {
		t.Fatalf("turn: %q", turn)
	}

	f.del.busy["arch"] = true
	q, _ := f.svc.SendToArchitect(ctx, "dev", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: "which branch?"})
	if q.Delivered || !q.Queued {
		t.Fatalf("a busy architect gets it later: %+v", q)
	}
	f.del.endTurn("arch")
	list, _ := f.svc.ListTaskMessages(ctx, f.ticket, ports.TaskMessageFilter{})
	if len(list) != 2 || !list[1].Delivered {
		t.Fatalf("delivered at turn end: %+v", list)
	}
	delivered := f.feed.count(func(c ports.ProjectChange) bool {
		return c.TaskMessage != nil && c.TaskMessage.ID == q.ID && c.TaskMessage.Delivered
	})
	if delivered != 1 {
		t.Fatalf("the feed announces it became delivered: %d", delivered)
	}

	f.end(t, "arch", domain.SessionStopped)
	s, _ := f.svc.SendToArchitect(ctx, "dev", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: "hello?"})
	if s.Delivered || s.Queued {
		t.Fatalf("a stopped architect: stored, not queued: %+v", s)
	}
}

func TestMessages_ValidationAndVisibility(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, intp(0))
	f.start(t, "a", "arch", "", intp(0))
	f.start(t, "b", "arch", "", intp(0))

	_, err := f.svc.SendToArchitect(ctx, "arch", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: "me?"})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.SendToArchitect(ctx, "a", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: " "})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.SendToArchitect(ctx, "a", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: "x", DocumentIDs: []string{"nope"}})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.SendToArchitect(ctx, "a", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: "x", ArtifactIDs: []string{"nope"}})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.SendToArchitect(ctx, "a", ports.TaskMessageInput{Kind: domain.MessageReply, Body: "x"})
	wantCode(t, err, "INVALID_INPUT")

	doc, _ := f.plan.CreateDocument(f.project, "Plan", "<p>x</p>", domain.DocumentFormatHTML, domain.DocumentScopeTask)
	_ = f.plan.LinkDocument(f.ticket, doc.ID)
	rq, err := f.svc.SendToArchitect(ctx, "a", ports.TaskMessageInput{Kind: domain.MessageReviewRequest, Body: "review my plan", DocumentIDs: []string{doc.ID}})
	if err != nil || len(rq.DocumentIDs) != 1 {
		t.Fatalf("a linked document is fine: %+v %v", rq, err)
	}
	_, _ = f.svc.SendToArchitect(ctx, "b", ports.TaskMessageInput{Kind: domain.MessageQuestion, Body: "b asks"})

	_, err = f.svc.ReplyFromArchitect(ctx, "a", "b", ports.TaskMessageInput{Body: "x"})
	wantCode(t, err, "ARCHITECT_ONLY")
	_, err = f.svc.ReplyFromArchitect(ctx, "arch", "ghost", ports.TaskMessageInput{Body: "x"})
	wantCode(t, err, "SESSION_NOT_ON_TASK")
	_, err = f.svc.ReplyFromArchitect(ctx, "arch", "a", ports.TaskMessageInput{Body: "x", InReplyTo: "ghost"})
	wantCode(t, err, "MESSAGE_NOT_FOUND")
	_, err = f.svc.ReplyFromArchitect(ctx, "arch", "a", ports.TaskMessageInput{Body: "x", Verdict: domain.VerdictApproved})
	wantCode(t, err, "INVALID_INPUT")
	r, err := f.svc.ReplyFromArchitect(ctx, "arch", "a", ports.TaskMessageInput{Body: "looks good", InReplyTo: rq.ID, Verdict: domain.VerdictApproved})
	if err != nil || r.Kind != domain.MessageReply || r.Verdict != domain.VerdictApproved || !r.Delivered {
		t.Fatalf("reply: %+v %v", r, err)
	}
	if !strings.Contains(f.del.got("a")[0], "reply/approved · from claude session arch]\nIn reply to "+rq.ID) {
		t.Fatalf("reply turn: %q", f.del.got("a")[0])
	}

	all, _ := f.svc.ListSessionMessages(ctx, "arch", ports.TaskMessageFilter{})
	mine, _ := f.svc.ListSessionMessages(ctx, "b", ports.TaskMessageFilter{SessionID: "a"})
	if len(all) != 3 || len(mine) != 1 || mine[0].FromSessionID != "b" {
		t.Fatalf("architect sees %d, b sees %+v", len(all), mine)
	}
	narrowed, _ := f.svc.ListSessionMessages(ctx, "arch", ports.TaskMessageFilter{SessionID: "a"})
	if len(narrowed) != 2 {
		t.Fatalf("architect narrowed to a: %d", len(narrowed))
	}
}

func TestStatusAuthority(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	f.start(t, "peer", "", "", nil)

	_, err := f.svc.SetTaskStatus(ctx, "dev", domain.TicketStatusReview, "")
	wantCode(t, err, "TASK_STATUS_OWNED_BY_ARCHITECT")
	_, err = f.svc.SetTaskStatus(ctx, "peer", domain.TicketStatusReview, "")
	wantCode(t, err, "TASK_STATUS_OWNED_BY_ARCHITECT")
	_, err = f.svc.SetTaskStatus(ctx, "arch", domain.TicketStatusReview, strings.Repeat("x", 281))
	wantCode(t, err, "INVALID_INPUT")
	tk, err := f.svc.SetTaskStatus(ctx, "arch", domain.TicketStatusReview, "plans in")
	if err != nil || tk.Status != domain.TicketStatusReview {
		t.Fatalf("the architect moves it: %+v %v", tk, err)
	}
	n := f.feed.count(func(c ports.ProjectChange) bool {
		return c.TaskStatus != nil && c.TaskStatus.By == domain.ChangedBySession && c.TaskStatus.BySessionID == "arch" && c.TaskStatus.Reason == "plans in"
	})
	if n != 1 {
		t.Fatalf("task_status on the feed: %d", n)
	}
	// A person still can, through planning.
	if _, err := f.plan.PatchTicket(ctx, f.ticket, ports.TicketPatch{Status: ptr(domain.TicketStatusDone)}); err != nil {
		t.Fatal(err)
	}
	if f.feed.count(func(c ports.ProjectChange) bool {
		return c.TaskStatus != nil && c.TaskStatus.By == domain.ChangedByPerson
	}) != 1 {
		t.Fatal("a person's move is announced as by person")
	}
}

func ptr[T any](v T) *T { return &v }

func TestReviews(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, intp(0))
	f.start(t, "dev", "arch", "", intp(0))

	_, err := f.svc.RequestUserReview(ctx, "dev", ports.ReviewRequestInput{Subject: "s", Body: "b"})
	wantCode(t, err, "ARCHITECT_ONLY")
	_, err = f.svc.RequestUserReview(ctx, "arch", ports.ReviewRequestInput{Subject: "", Body: "b"})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.RequestUserReview(ctx, "arch", ports.ReviewRequestInput{Subject: "s", Body: "b", AboutSessionID: "ghost"})
	wantCode(t, err, "SESSION_NOT_ON_TASK")

	r, err := f.svc.RequestUserReview(ctx, "arch", ports.ReviewRequestInput{Subject: "Server plan", Body: "Please look", AboutSessionID: "dev"})
	if err != nil || r.State != domain.ReviewPending {
		t.Fatalf("request: %+v %v", r, err)
	}
	tk, _ := f.plan.GetTicket(ctx, f.ticket)
	if tk.PendingReviews != 1 || tk.ArchitectSessionID == nil || *tk.ArchitectSessionID != "arch" {
		t.Fatalf("ticket: %+v", tk)
	}
	if f.feed.count(func(c ports.ProjectChange) bool { return c.Ticket != nil && c.Ticket.PendingReviews == 1 }) == 0 {
		t.Fatal("a pending count change re-announces the ticket")
	}
	_, _, err = f.svc.RespondReview(ctx, r.ID, domain.ReviewChangesRequested, "")
	wantCode(t, err, "INVALID_INPUT")
	_, _, err = f.svc.RespondReview(ctx, r.ID, domain.ReviewWithdrawn, "x")
	wantCode(t, err, "INVALID_INPUT")
	_, _, err = f.svc.RespondReview(ctx, "ghost", domain.ReviewApproved, "")
	wantCode(t, err, "REVIEW_NOT_FOUND")

	got, delivered, err := f.svc.RespondReview(ctx, r.ID, domain.ReviewChangesRequested, "split step 6")
	if err != nil || !delivered || got.State != domain.ReviewChangesRequested || got.RespondedAt == nil {
		t.Fatalf("respond: %+v %v %v", got, delivered, err)
	}
	if turn := f.del.got("arch")[0]; turn != "[review response "+r.ID+" · review_response · changes_requested]\nSubject: Server plan\nsplit step 6" {
		t.Fatalf("turn: %q", turn)
	}
	_, _, err = f.svc.RespondReview(ctx, r.ID, domain.ReviewApproved, "")
	wantCode(t, err, "REVIEW_NOT_PENDING")
	_, err = f.svc.WithdrawReview(ctx, "arch", r.ID)
	wantCode(t, err, "REVIEW_NOT_PENDING")

	f.advance(time.Minute)
	r2, _ := f.svc.RequestUserReview(ctx, "arch", ports.ReviewRequestInput{Subject: "two", Body: "b"})
	w, err := f.svc.WithdrawReview(ctx, "arch", r2.ID)
	if err != nil || w.State != domain.ReviewWithdrawn {
		t.Fatalf("withdraw: %+v %v", w, err)
	}
	list, _ := f.svc.ListReviewRequests(ctx, f.ticket, "")
	inbox, _ := f.svc.ListProjectReviewRequests(ctx, f.project, domain.ReviewPending)
	if len(list) != 2 || list[0].ID != r2.ID || len(inbox) != 0 {
		t.Fatalf("list %+v inbox %+v", list, inbox)
	}
	_, err = f.svc.ListReviewRequests(ctx, f.ticket, "bogus")
	wantCode(t, err, "INVALID_INPUT")

	// A stopped architect: the decision is stored, not delivered.
	r3, _ := f.svc.RequestUserReview(ctx, "arch", ports.ReviewRequestInput{Subject: "three", Body: "b"})
	f.end(t, "arch", domain.SessionStopped)
	if _, delivered, _ := f.svc.RespondReview(ctx, r3.ID, domain.ReviewApproved, ""); delivered {
		t.Fatal("not delivered to a stopped architect")
	}
}

func TestDecorateSessions(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	f.start(t, "sub", "dev", "", nil)
	f.start(t, "peer", "", "", nil)
	all := f.sessions.List(ports.SessionFilter{TicketID: f.ticket})
	f.svc.DecorateSessions(all...)
	want := map[string]domain.SessionRole{"arch": domain.RoleArchitect, "dev": domain.RoleDelegate, "sub": domain.RoleDelegate, "peer": domain.RolePeer}
	for _, s := range all {
		if s.Role != want[s.ID] || s.ArchitectSessionID == nil || *s.ArchitectSessionID != "arch" {
			t.Errorf("%s: %q %v", s.ID, s.Role, s.ArchitectSessionID)
		}
		if (s.StatusCheck != nil) != (s.ID == "dev") {
			t.Errorf("%s: status check %+v (only the architect's direct delegate has one)", s.ID, s.StatusCheck)
		}
	}
}
