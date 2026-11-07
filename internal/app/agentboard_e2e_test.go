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

	"github.com/rfbatista/llmkit/approval"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentboard"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// boardE2E is the server wired as it runs, with the HTTP API of the Agent
// board on a test server. Sessions are recorded on the stores and announced
// the way the agent host announces them.
type boardE2E struct {
	t        *testing.T
	ctx      context.Context
	cat      catalog.Catalog
	orch     *orchestration.Service
	plan     *planning.Service
	sessions ports.SessionRepository
	broker   *approval.Broker
	srv      *httptest.Server
	events   <-chan domain.BoardEvent
	lastSeq  int64
}

func newBoardE2E(t *testing.T) *boardE2E {
	t.Helper()
	e := &boardE2E{t: t, ctx: context.Background()}
	var board *agentboard.Service
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: t.TempDir(), ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, AgentBoardModule,
		fx.Populate(&e.orch, &e.plan, &e.cat, &e.sessions, &e.broker, &board),
		fx.NopLogger,
	)
	if err := app.Start(e.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	e.srv = httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{AgentBoard: board})))
	t.Cleanup(e.srv.Close)
	return e
}

// followEvents reads the live session feed's own SSE endpoint for the rest of
// the test, checking that it writes bare data lines.
func (e *boardE2E) followEvents() {
	ctx, cancel := context.WithCancel(e.ctx)
	e.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/live_sessions/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	out := make(chan domain.BoardEvent, 1024)
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				e.t.Errorf("not a bare data line: %q", line)
				return
			}
			var ev domain.BoardEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				e.t.Errorf("event %q: %v", data, err)
				return
			}
			out <- ev
		}
	}()
	e.events = out
}

func (e *boardE2E) seed() domain.BoardSeed {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/agent_board")
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("GET /api/agent_board: %d", res.StatusCode)
	}
	var s domain.BoardSeed
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// next is the next event on the feed, which must follow the last by one.
func (e *boardE2E) next() domain.BoardEvent {
	e.t.Helper()
	select {
	case ev := <-e.events:
		if e.lastSeq != 0 && ev.Seq != e.lastSeq+1 {
			e.t.Fatalf("seq %d after %d: not one more", ev.Seq, e.lastSeq)
		}
		e.lastSeq = ev.Seq
		return ev
	case <-time.After(5 * time.Second):
		e.t.Fatal("no event on the live session feed")
		return domain.BoardEvent{}
	}
}

func (e *boardE2E) project(name string) string {
	e.t.Helper()
	p, err := e.cat.Projects.CreateProject(e.ctx, name, e.t.TempDir())
	if err != nil {
		e.t.Fatal(err)
	}
	return p.ID
}

func (e *boardE2E) task(projectID, title, parentID string) string {
	e.t.Helper()
	tk, err := e.plan.CreateTicket(e.ctx, projectID, title, "", domain.TicketStatusTodo, parentID)
	if err != nil {
		e.t.Fatal(err)
	}
	return tk.ID
}

type fakeSession struct {
	agent  string
	status domain.SessionStatus
	parent string
	mode   domain.SessionMode
}

// start records a session on the task and announces it.
func (e *boardE2E) start(projectID, taskID string, f fakeSession) string {
	e.t.Helper()
	if f.status == "" {
		f.status = domain.SessionRunning
	}
	s, err := e.sessions.Create(&domain.Session{ProjectID: projectID, TicketID: taskID, AgentID: f.agent, Task: "t",
		WorkingDir: e.t.TempDir(), Status: f.status, ParentSessionID: f.parent, Mode: f.mode, Model: "opus"})
	if err != nil {
		e.t.Fatal(err)
	}
	e.orch.AnnounceSession(s.ID)
	time.Sleep(3 * time.Millisecond) // statuses are timed to the millisecond
	return s.ID
}

func (e *boardE2E) setStatus(sessionID string, status domain.SessionStatus) {
	e.t.Helper()
	if err := e.sessions.UpdateStatus(sessionID, status); err != nil {
		e.t.Fatal(err)
	}
	e.orch.AnnounceSession(sessionID)
	time.Sleep(3 * time.Millisecond)
}

func cardOf(s domain.BoardSeed, taskID string) *domain.BoardCard {
	for _, p := range s.Projects {
		for i := range p.Cards {
			if p.Cards[i].TaskID == taskID {
				return &p.Cards[i]
			}
		}
	}
	return nil
}

func taskIDs(cards []domain.BoardCard) []string {
	var out []string
	for _, c := range cards {
		out = append(out, c.TaskID)
	}
	return out
}

func sameIDs(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s: got %v want %v", what, got, want)
	}
}

// TestAgentBoardSeed_ListsTasksWithLiveSessions_EndToEnd reads the seed over
// HTTP: only tasks with a live session, by project name, needing-you first,
// with the card and session line fields of the contract.
func TestAgentBoardSeed_ListsTasksWithLiveSessions_EndToEnd(t *testing.T) {
	e := newBoardE2E(t)
	agent, err := e.cat.Agents.CreateAgent(e.ctx, "go-developer", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentID := agent.ID
	zeta, alpha := e.project("zeta"), e.project("Alpha")
	e.project("quiet") // nothing live
	ended := e.project("ended")

	// Alpha: a waits on the person; b runs; c's session is done; d has none.
	a, b := e.task(alpha, "a", ""), e.task(alpha, "b", "")
	c, d := e.task(alpha, "c", ""), e.task(alpha, "d", "")
	_ = d
	e.start(alpha, a, fakeSession{status: domain.SessionIdle})
	e.start(alpha, b, fakeSession{agent: agentID})
	e.start(alpha, c, fakeSession{status: domain.SessionDone})
	e.start(alpha, "", fakeSession{}) // a session with no task
	// Zeta: a feature with a child on a thinking session, and a second task
	// that started needing the person later than a's wait.
	feature := e.task(zeta, "the feature", "")
	child := e.task(zeta, "the child", feature)
	e.start(zeta, child, fakeSession{status: domain.SessionThinking})
	later := e.task(zeta, "later", "")
	e.start(zeta, later, fakeSession{status: domain.SessionWaitingApproval})
	// A task of another project with only ended sessions: stays off.
	for _, st := range []domain.SessionStatus{domain.SessionFailed, domain.SessionStopped} {
		e.start(ended, e.task(ended, "x", ""), fakeSession{status: st})
	}
	// Two sessions on one task: the newest status change first; roles.
	arch := e.start(alpha, b, fakeSession{mode: domain.SessionModeArchitect, status: domain.SessionPaused})
	e.start(alpha, b, fakeSession{parent: arch, status: domain.SessionStarting})

	s := e.seed()
	if s.ProjectCount != 4 || s.QuietProjects != 2 {
		t.Fatalf("project_count %d quiet %d, want 4 and 2", s.ProjectCount, s.QuietProjects)
	}
	if len(s.Projects) != 2 || s.Projects[0].ProjectName != "Alpha" || s.Projects[1].ProjectName != "zeta" {
		t.Fatalf("projects: %+v", s.Projects)
	}
	// Alpha: a needs you, so it is first; then b.
	sameIDs(t, "alpha cards", taskIDs(s.Projects[0].Cards), []string{a, b})
	ca := cardOf(s, a)
	if !ca.NeedsYou || ca.NeedsYouSince == nil || !ca.NeedsYouSince.Equal(ca.Sessions[0].StatusSince) {
		t.Fatalf("a: %+v", ca)
	}
	cb := cardOf(s, b)
	if cb.NeedsYou || cb.NeedsYouSince != nil || len(cb.Sessions) != 3 {
		t.Fatalf("b: %+v", cb)
	}
	// Newest status change first, and the card's last change is the first's.
	if cb.Sessions[0].Status != domain.SessionStarting || !cb.LastStatusChangeAt.Equal(cb.Sessions[0].StatusSince) {
		t.Fatalf("b sessions: %+v", cb.Sessions)
	}
	roles := map[domain.SessionStatus][2]string{}
	for _, l := range cb.Sessions {
		roles[l.Status] = [2]string{l.Role, l.AgentName}
	}
	if roles[domain.SessionStarting] != [2]string{"delegate", "claude"} ||
		roles[domain.SessionPaused] != [2]string{"architect", "claude"} ||
		roles[domain.SessionRunning] != [2]string{"peer", "go-developer"} {
		t.Fatalf("roles and agents: %v", roles)
	}
	if l := cb.Sessions[2]; l.Model != "opus" || l.Mode != "" {
		t.Fatalf("model/mode: %+v", l)
	}
	// Zeta: the one that waits comes before the one that thinks, and the
	// child carries its feature.
	sameIDs(t, "zeta cards", taskIDs(s.Projects[1].Cards), []string{later, child})
	cc := cardOf(s, child)
	if cc.FeatureID == nil || *cc.FeatureID != feature || *cc.FeatureTitle != "the feature" {
		t.Fatalf("child: %+v", cc)
	}
	if cardOf(s, feature) != nil || cardOf(s, c) != nil || cardOf(s, d) != nil {
		t.Fatal("a task with no live session is on the board")
	}
	if cardOf(s, later).Sessions[0].WaitingFor != nil {
		t.Fatal("waiting_for is set though no approval is pending")
	}
	// The seed is the feed's number so far: nothing has followed yet.
	if s.Seq < 0 {
		t.Fatalf("seq %d", s.Seq)
	}
}

// TestLiveSessionFeed_EmitsTheContractsEvents_EndToEnd drives sessions through
// start, status changes, an approval, end, delete and a task deletion, and
// edits tasks and projects, reading the feed's own SSE endpoint.
func TestLiveSessionFeed_EmitsTheContractsEvents_EndToEnd(t *testing.T) {
	e := newBoardE2E(t)
	proj := e.project("harness")
	other := e.project("other")
	tk, plain := e.task(proj, "first", ""), e.task(proj, "no session", "")
	e.followEvents()

	// A session starts: its card arrives with the project's name.
	s1 := e.start(proj, tk, fakeSession{})
	ev := e.next()
	if ev.Kind != domain.BoardSessionLive || ev.TaskID != tk || ev.ProjectID != proj || ev.ProjectName != "harness" ||
		ev.Session == nil || ev.Session.SessionID != s1 || ev.Session.Status != domain.SessionRunning ||
		ev.Card == nil || len(ev.Card.Sessions) != 1 || ev.Card.NeedsYou {
		t.Fatalf("start: %+v", ev)
	}
	startSeq := ev.Seq
	// The seed read right after an event is at that event's number.
	if seed := e.seed(); seed.Seq != startSeq || cardOf(seed, tk) == nil {
		t.Fatalf("seed seq %d, want %d", seed.Seq, startSeq)
	}

	// A status that does not change sends nothing; a real change does.
	e.setStatus(s1, domain.SessionRunning)
	e.setStatus(s1, domain.SessionIdle)
	ev = e.next()
	if ev.Kind != domain.BoardSessionLive || ev.Session.Status != domain.SessionIdle || !ev.Card.NeedsYou ||
		ev.Card.NeedsYouSince == nil || !ev.Card.NeedsYouSince.Equal(ev.Session.StatusSince) {
		t.Fatalf("idle: %+v", ev)
	}
	since := *ev.Card.NeedsYouSince

	// An approval: the line says what it asks for, cut at 120 characters,
	// and the card keeps needing the person since the first time.
	command := "git log " + strings.Repeat("--oneline ", 30)
	done := make(chan approval.Decision, 1)
	go func() {
		done <- e.broker.Await(e.ctx, s1, "req-1", "Bash", json.RawMessage(`{"command":"`+command+`"}`))
	}()
	ev = e.next()
	w := ev.Session.WaitingFor
	if ev.Session.Status != domain.SessionWaitingApproval || w == nil || len([]rune(*w)) != 120 ||
		!strings.HasPrefix(*w, "Bash: git log --oneline") || !strings.HasSuffix(*w, "…") {
		t.Fatalf("approval: %+v waiting_for %v", ev.Session, w)
	}
	if !ev.Card.NeedsYou || !ev.Card.NeedsYouSince.Equal(since) {
		t.Fatalf("needs_you_since moved: %+v", ev.Card)
	}
	if err := e.orch.Resolve(e.ctx, s1, "req-1", true, ""); err != nil {
		t.Fatal(err)
	}
	if d := <-done; !d.Allow {
		t.Fatal("not allowed")
	}
	ev = e.next()
	if ev.Session.Status != domain.SessionThinking || ev.Session.WaitingFor != nil || ev.Card.NeedsYou || ev.Card.NeedsYouSince != nil {
		t.Fatalf("resolved: %+v", ev)
	}

	// A second session, then each way of ending.
	s2 := e.start(proj, tk, fakeSession{})
	if ev = e.next(); ev.Kind != domain.BoardSessionLive || ev.SessionID != s2 || len(ev.Card.Sessions) != 2 {
		t.Fatalf("second: %+v", ev)
	}
	e.setStatus(s2, domain.SessionFailed)
	ev = e.next()
	if ev.Kind != domain.BoardSessionEnded || ev.SessionID != s2 || ev.Ended != "failed" || ev.Card == nil || len(ev.Card.Sessions) != 1 {
		t.Fatalf("failed: %+v", ev)
	}
	e.setStatus(s1, domain.SessionDone)
	ev = e.next()
	if ev.Kind != domain.BoardSessionEnded || ev.SessionID != s1 || ev.Ended != "done" || ev.Card != nil {
		t.Fatalf("done: %+v", ev)
	}
	// Resumed from an ended status: live again.
	e.setStatus(s1, domain.SessionRunning)
	if ev = e.next(); ev.Kind != domain.BoardSessionLive || ev.SessionID != s1 || ev.Card == nil {
		t.Fatalf("resumed: %+v", ev)
	}
	e.setStatus(s1, domain.SessionStopped)
	if ev = e.next(); ev.Kind != domain.BoardSessionEnded || ev.Ended != "stopped" || ev.Card != nil {
		t.Fatalf("stopped: %+v", ev)
	}
	s3 := e.start(proj, tk, fakeSession{})
	e.next()
	if err := e.orch.Delete(e.ctx, s3); err != nil {
		t.Fatal(err)
	}
	if ev = e.next(); ev.Kind != domain.BoardSessionEnded || ev.SessionID != s3 || ev.Ended != "deleted" || ev.Card != nil {
		t.Fatalf("deleted: %+v", ev)
	}

	// Task changes: only a task with a live session is on the feed.
	e.start(proj, tk, fakeSession{})
	e.next()
	if _, err := e.plan.UpdateTicket(e.ctx, plain, "renamed, no session", "", domain.TicketStatusInProgress); err != nil {
		t.Fatal(err)
	}
	if _, err := e.plan.UpdateTicket(e.ctx, tk, "first, renamed", "", domain.TicketStatusReview); err != nil {
		t.Fatal(err)
	}
	ev = e.next() // the next event is the live task's: the other sent none
	if ev.Kind != domain.BoardTaskChanged || ev.TaskID != tk || ev.Card.TaskTitle != "first, renamed" || ev.Card.TaskStatus != "review" {
		t.Fatalf("task: %+v", ev)
	}
	// A feature's title is on its child's card.
	feature := e.task(proj, "feature", "")
	child := e.task(proj, "child", feature)
	e.start(proj, child, fakeSession{})
	e.next()
	if _, err := e.plan.UpdateTicket(e.ctx, feature, "feature, renamed", "", domain.TicketStatusTodo); err != nil {
		t.Fatal(err)
	}
	if ev = e.next(); ev.Kind != domain.BoardTaskChanged || ev.TaskID != child || *ev.Card.FeatureTitle != "feature, renamed" {
		t.Fatalf("feature: %+v", ev)
	}

	// A project rename: only a project with a live session is on the feed.
	if _, err := e.cat.Projects.UpdateProject(e.ctx, other, "other, renamed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cat.Projects.UpdateProject(e.ctx, proj, "harness 2", ""); err != nil {
		t.Fatal(err)
	}
	ev = e.next()
	if ev.Kind != domain.BoardProjectChanged || ev.ProjectID != proj || ev.ProjectName != "harness 2" {
		t.Fatalf("project: %+v", ev)
	}

	// The task of a live session is deleted.
	gone := e.task(proj, "gone", "")
	e.start(proj, gone, fakeSession{})
	e.next()
	if err := e.plan.DeleteTicket(e.ctx, gone); err != nil {
		t.Fatal(err)
	}
	ev = e.next()
	if ev.Kind != domain.BoardSessionEnded || ev.TaskID != gone || ev.Ended != "deleted" || ev.Card != nil {
		t.Fatalf("task deleted: %+v", ev)
	}
	if seed := e.seed(); seed.Seq != ev.Seq {
		t.Fatalf("seed seq %d, last event %d", seed.Seq, ev.Seq)
	}
}

// TestLiveSessionFeed_IsFollowableOnThePageStream_EndToEnd follows the feed on
// /api/stream with the path the contract names.
func TestLiveSessionFeed_IsFollowableOnThePageStream_EndToEnd(t *testing.T) {
	e := newBoardE2E(t)
	proj := e.project("harness")
	tk := e.task(proj, "first", "")

	ctx, cancel := context.WithCancel(e.ctx)
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/stream", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
				lines <- data
			}
		}
	}()
	var first struct {
		Stream string `json:"stream"`
	}
	select {
	case l := <-lines:
		if err := json.Unmarshal([]byte(l), &first); err != nil || first.Stream == "" {
			t.Fatalf("first event %q", l)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no stream id")
	}
	body := strings.NewReader(`{"stream":"` + first.Stream + `","path":"/live_sessions/events"}`)
	post, err := http.Post(e.srv.URL+"/api/stream/follow", "application/json", body)
	if err != nil || post.StatusCode != http.StatusOK {
		t.Fatalf("follow: %v %v", err, post)
	}
	post.Body.Close()

	id := e.start(proj, tk, fakeSession{status: domain.SessionIdle})
	select {
	case l := <-lines:
		var m struct {
			Path string            `json:"path"`
			Data domain.BoardEvent `json:"data"`
		}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		if m.Path != "/live_sessions/events" || m.Data.Kind != domain.BoardSessionLive || m.Data.SessionID != id ||
			m.Data.Seq != 1 || !m.Data.Card.NeedsYou {
			t.Fatalf("stream event: %s", l)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event on the page stream")
	}
}
