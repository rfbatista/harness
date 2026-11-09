//go:build !windows

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentboard"
	"operators-mcp/internal/application/interviews"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// signalsE2E is the server wired as it runs, with the HTTP API of sessions,
// the project's feed, the Agent board, the interviews and the inbox on a test
// server. Sessions are recorded on the store the way the agent host records
// them; interviews are asked through the service llmkit's Sink calls.
type signalsE2E struct {
	t        *testing.T
	ctx      context.Context
	cat      catalog.Catalog
	orch     *orchestration.Service
	plan     *planning.Service
	sessions ports.SessionRepository
	board    *agentboard.Service
	svc      *interviews.Service
	inbox    *notifications.Service
	srv      *httptest.Server
	project  string
	task     string
}

func newSignalsE2E(t *testing.T) *signalsE2E {
	t.Helper()
	e := &signalsE2E{t: t, ctx: context.Background()}
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: t.TempDir(), ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule, AgentBoardModule, InterviewsModule,
		fx.Populate(&e.orch, &e.plan, &e.cat, &e.sessions, &e.board, &e.svc, &e.inbox),
		fx.NopLogger,
	)
	if err := app.Start(e.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	e.srv = httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Sessions: e.orch, AgentBoard: e.board, Interviews: e.svc, Notifications: e.inbox,
	})))
	t.Cleanup(e.srv.Close)
	p, err := e.cat.Projects.CreateProject(e.ctx, "harness", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.project = p.ID
	tk, err := e.plan.CreateTicket(e.ctx, p.ID, "the task", "", domain.TicketStatusTodo, "")
	if err != nil {
		t.Fatal(err)
	}
	e.task = tk.ID
	return e
}

// start records a live interactive session on the task, as a person's
// terminal runs it, and announces it.
func (e *signalsE2E) start(status domain.SessionStatus) string {
	e.t.Helper()
	s, err := e.sessions.Create(&domain.Session{ProjectID: e.project, TicketID: e.task, Task: "t", WorkingDir: e.t.TempDir(),
		Status: status, Interactive: true, RunsOn: domain.RunnerTUI})
	if err != nil {
		e.t.Fatal(err)
	}
	e.orch.AnnounceSession(s.ID)
	time.Sleep(3 * time.Millisecond) // statuses are timed to the millisecond
	return s.ID
}

func (e *signalsE2E) setStatus(id string, status domain.SessionStatus) {
	e.t.Helper()
	if err := e.sessions.UpdateStatus(id, status); err != nil {
		e.t.Fatal(err)
	}
	e.orch.AnnounceSession(id)
	time.Sleep(3 * time.Millisecond)
}

func (e *signalsE2E) get(path string, into any) {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("GET %s: %d", path, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		e.t.Fatal(err)
	}
}

// session reads the session over HTTP, as the session screen's head does.
func (e *signalsE2E) session(id string) domain.Session {
	e.t.Helper()
	var out struct {
		Session domain.Session `json:"session"`
	}
	e.get("/api/sessions/"+id, &out)
	return out.Session
}

// listed reads the session in the project's session list, as a row does.
func (e *signalsE2E) listed(id string) domain.Session {
	e.t.Helper()
	var out struct {
		Sessions []domain.Session `json:"sessions"`
	}
	e.get("/api/sessions?project_id="+e.project, &out)
	for _, s := range out.Sessions {
		if s.ID == id {
			return s
		}
	}
	e.t.Fatalf("session %s not in the list", id)
	return domain.Session{}
}

func (e *signalsE2E) card() *domain.BoardCard {
	e.t.Helper()
	var seed domain.BoardSeed
	e.get("/api/agent_board", &seed)
	return cardOf(seed, e.task)
}

func (e *signalsE2E) ask(id, session string) *domain.Interview {
	e.t.Helper()
	got, err := e.svc.Ask(e.ctx, interview(id, session))
	if err != nil {
		e.t.Fatalf("ask %s: %v", id, err)
	}
	return got
}

// read reads the interviews of the session over HTTP.
func (e *signalsE2E) read(session string) domain.InterviewList {
	e.t.Helper()
	var list domain.InterviewList
	e.get("/api/sessions/"+session+"/interviews", &list)
	return list
}

// projectFeed follows the project's session changes on their own SSE
// endpoint, for the rest of the test.
func (e *signalsE2E) projectFeed() <-chan domain.Session {
	e.t.Helper()
	ctx, cancel := context.WithCancel(e.ctx)
	e.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events?project_id="+e.project, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	out := make(chan domain.Session, 256)
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data:")
			if !ok {
				continue
			}
			var ch struct {
				Session *domain.Session `json:"session"`
			}
			if json.Unmarshal([]byte(data), &ch) == nil && ch.Session != nil {
				out <- *ch.Session
			}
		}
	}()
	return out
}

// feedSession waits for the feed to carry the session with the counts.
func feedSession(t *testing.T, feed <-chan domain.Session, id string, interviews, questions int) domain.Session {
	t.Helper()
	deadline := time.After(5 * time.Second)
	var last domain.Session
	for {
		select {
		case s := <-feed:
			if s.ID != id {
				continue
			}
			last = s
			if s.OpenInterviewCount == interviews && s.OpenQuestionCount == questions {
				return s
			}
		case <-deadline:
			t.Fatalf("the session feed never carried %d interviews and %d questions for %s; last: %+v", interviews, questions, id, last)
		}
	}
}

func (e *signalsE2E) notificationsOf(session string) []*domain.Notification {
	e.t.Helper()
	var page ports.NotificationPage
	e.get("/api/notifications?project_id="+e.project, &page)
	var out []*domain.Notification
	for _, n := range page.Notifications {
		if n.SessionID == session {
			out = append(out, n)
		}
	}
	return out
}

// An interview asked on a running session shows on its head, its row, the
// project's feed and the board: counts, and a card that needs the person.
func TestAnOpenInterviewMakesARunningSessionNeedThePerson(t *testing.T) {
	e := newSignalsE2E(t)
	id := e.start(domain.SessionRunning)
	feed := e.projectFeed()
	if c := e.card(); c == nil || c.NeedsYou {
		t.Fatalf("before the ask: card %+v", c)
	}

	asked := e.ask("iv-1", id)

	for where, s := range map[string]domain.Session{"head": e.session(id), "row": e.listed(id)} {
		if s.OpenInterviewCount != 1 || s.OpenQuestionCount != 2 || s.Status != domain.SessionRunning {
			t.Errorf("%s: open_interview_count %d, open_question_count %d, status %s", where, s.OpenInterviewCount, s.OpenQuestionCount, s.Status)
		}
	}
	if s := feedSession(t, feed, id, 1, 2); s.Status != domain.SessionRunning {
		t.Errorf("feed: status %s", s.Status)
	}
	c := e.card()
	if c == nil || !c.NeedsYou || c.NeedsYouSince == nil || !c.NeedsYouSince.Equal(asked.AskedAt.Truncate(time.Millisecond)) {
		t.Fatalf("card after the ask: %+v (asked_at %v)", c, asked.AskedAt)
	}
	if l := c.Sessions[0]; l.OpenInterviewCount != 1 || l.OpenQuestionCount != 2 {
		t.Errorf("session line: %+v", l)
	}

	// A second interview adds to both counts.
	e.ask("iv-2", id)
	feedSession(t, feed, id, 2, 4)
	if s := e.session(id); s.OpenInterviewCount != 2 || s.OpenQuestionCount != 4 {
		t.Errorf("head after a second ask: %+v", s)
	}
}

// Answering or dismissing an interview, however it is changed to a state
// other than open, takes it out of the counts and off the board's wait.
func TestChangingAnInterviewOutOfOpenLowersTheCounts(t *testing.T) {
	e := newSignalsE2E(t)
	id := e.start(domain.SessionRunning)
	feed := e.projectFeed()
	first := e.ask("iv-1", id)
	second := e.ask("iv-2", id)
	feedSession(t, feed, id, 2, 4)

	closed := *first
	now := time.Now()
	closed.State, closed.EndedAt = domain.InterviewDismissed, &now
	if _, err := e.svc.Change(e.ctx, closed); err != nil {
		t.Fatal(err)
	}
	feedSession(t, feed, id, 1, 2)
	// The card still needs the person, and since the first ask: it did not
	// start needing them again.
	if c := e.card(); !c.NeedsYou || !c.NeedsYouSince.Equal(first.AskedAt.Truncate(time.Millisecond)) {
		t.Fatalf("card with one left: %+v", c)
	}

	closed = *second
	closed.State, closed.EndedAt = domain.InterviewSent, &now
	if _, err := e.svc.Change(e.ctx, closed); err != nil {
		t.Fatal(err)
	}
	feedSession(t, feed, id, 0, 0)
	if c := e.card(); c.NeedsYou || c.NeedsYouSince != nil {
		t.Fatalf("card with none left: %+v", c)
	}
}

// needs_you_since is the earliest of the signals: an interview asked before
// another session of the task went idle is the one the card has waited on.
func TestTheBoardCardWaitsSinceTheEarliestSignal(t *testing.T) {
	e := newSignalsE2E(t)
	running := e.start(domain.SessionRunning)
	asked := e.ask("iv-1", running)
	idle := e.start(domain.SessionIdle) // idle later than the ask

	c := e.card()
	if !c.NeedsYou || c.NeedsYouSince == nil || !c.NeedsYouSince.Equal(asked.AskedAt.Truncate(time.Millisecond)) {
		t.Fatalf("card: %+v, want needs_you_since %v", c, asked.AskedAt)
	}
	_ = idle
}

// A task whose only waiting session was idle before the ask keeps that
// earlier time: the card did not start needing the person again.
func TestTheBoardCardKeepsAnEarlierIdleWait(t *testing.T) {
	e := newSignalsE2E(t)
	id := e.start(domain.SessionIdle)
	before := e.card()
	if before == nil || before.NeedsYouSince == nil {
		t.Fatalf("idle card: %+v", before)
	}
	e.ask("iv-1", id)
	if c := e.card(); !c.NeedsYou || !c.NeedsYouSince.Equal(*before.NeedsYouSince) {
		t.Fatalf("card after the ask: %+v, want since %v", c, before.NeedsYouSince)
	}
}

// Another session of the task, running, does not hide the interview's wait,
// and an approval still names what the session waits for.
func TestAnInterviewAndAnApprovalOrIdleBothNeedThePerson(t *testing.T) {
	e := newSignalsE2E(t)
	for _, status := range []domain.SessionStatus{domain.SessionIdle, domain.SessionWaitingApproval} {
		id := e.start(status)
		e.ask("iv-"+string(status), id)
		s := e.session(id)
		if s.Status != status || s.OpenInterviewCount != 1 {
			t.Errorf("%s: %+v", status, s)
		}
		if c := e.card(); !c.NeedsYou {
			t.Errorf("%s: card %+v", status, c)
		}
	}
}

// One inbox notification per interview: level action, linking to the
// session (the task page with ?session=<id>), and not again however the interview changes.
func TestEachInterviewPostsOneActionNotification(t *testing.T) {
	e := newSignalsE2E(t)
	id := e.start(domain.SessionRunning)
	first := e.ask("iv-1", id)
	e.ask("iv-2", id)

	ns := e.notificationsOf(id)
	if len(ns) != 2 {
		t.Fatalf("notifications: %d, want 2: %+v", len(ns), ns)
	}
	for _, n := range ns {
		if n.Level != domain.NotificationAction || n.Link != domain.TaskLink(e.project, e.task)+"?session="+id || n.TaskID != e.task || n.Title == "" {
			t.Errorf("notification: %+v", n)
		}
	}

	closed := *first
	now := time.Now()
	closed.State, closed.EndedAt = domain.InterviewDismissed, &now
	if _, err := e.svc.Change(e.ctx, closed); err != nil {
		t.Fatal(err)
	}
	e.setStatus(id, domain.SessionIdle)
	if got := e.notificationsOf(id); len(got) != 2 {
		t.Fatalf("after a change: %d notifications, want still 2", len(got))
	}
}

// Ending the session, however it ends, expires what it left open: the
// interview feed says so, and the session feeds say zero.
func TestEndingASessionExpiresItsOpenInterviews(t *testing.T) {
	for name, end := range map[string]struct {
		code   int
		user   bool
		status domain.SessionStatus
	}{
		"done":    {0, false, domain.SessionDone},
		"failed":  {1, false, domain.SessionFailed},
		"stopped": {0, true, domain.SessionStopped},
	} {
		t.Run(name, func(t *testing.T) {
			e := newSignalsE2E(t)
			id := e.start(domain.SessionRunning)
			e.ask("iv-open", id)
			sent := e.ask("iv-sent", id)
			closed := *sent
			at := time.Now()
			closed.State, closed.EndedAt = domain.InterviewSent, &at
			if _, err := e.svc.Change(e.ctx, closed); err != nil {
				t.Fatal(err)
			}
			feed := e.projectFeed()
			events, err := e.svc.FollowInterviews(e.ctx, id)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := e.orch.EndInteractive(e.ctx, id, end.code, end.user); err != nil {
				t.Fatal(err)
			}

			select {
			case ev := <-events:
				if ev.Kind != domain.InterviewChanged || ev.Interview.ID != "iv-open" || ev.Interview.State != domain.InterviewExpired || ev.Interview.EndedAt == nil {
					t.Fatalf("interview event: %+v %+v", ev, ev.Interview)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no interview_changed")
			}
			s := feedSession(t, feed, id, 0, 0)
			if s.Status != end.status {
				t.Errorf("feed status %s, want %s", s.Status, end.status)
			}
			list := e.read(id)
			if got := interviewOf(t, list, "iv-open"); got.State != domain.InterviewExpired || got.EndedAt == nil {
				t.Errorf("expired interview: %+v", got)
			}
			if got := interviewOf(t, list, "iv-sent"); got.State != domain.InterviewSent {
				t.Errorf("a sent interview stays sent: %+v", got)
			}
			if got := e.session(id); got.OpenInterviewCount != 0 || got.OpenQuestionCount != 0 {
				t.Errorf("head after the end: %+v", got)
			}
			select {
			case ev := <-events:
				t.Fatalf("a second event: %+v", ev)
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}
