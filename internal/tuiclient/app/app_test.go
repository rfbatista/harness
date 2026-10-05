//go:build !windows

package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/runtimetest"
	"operators-mcp/internal/tuiclient/tuitest"
)

// run drives the client the way the Bubble Tea runtime would: every
// returned command runs on its own goroutine and its message is fed back.
type run struct {
	t    *testing.T
	m    Model
	be   *tuitest.Fake
	msgs chan tea.Msg
	quit bool
}

// scripts maps a session's first message (or "resume") to the shell script
// its pane runs in place of claude.
func newRun(t *testing.T, be *tuitest.Fake, scripts map[string]string) *run {
	t.Helper()
	scripted(t, be, scripts)
	return startRun(t, be, depsOf(be, newHost(t)))
}

// newServerRun is a client whose sessions the server runs: be.Server stands
// in for the server's terminal host, and the client has none of its own.
func newServerRun(t *testing.T, be *tuitest.Fake, scripts map[string]string) *run {
	t.Helper()
	scripted(t, be, scripts)
	if be.Server == nil {
		be.Server = newHost(t)
	}
	d := depsOf(be, nil)
	d.RunsOn, d.RunnerHost = domain.RunnerServer, ""
	return startRun(t, be, d)
}

// scripted makes sessions run scripts[first message] (or scripts["resume"])
// in place of claude.
func scripted(t *testing.T, be *tuitest.Fake, scripts map[string]string) {
	be.Launch = func(s *domain.Session, resume bool) ports.AgentSpec {
		key := s.Task
		if resume {
			key = "resume"
		}
		script, ok := scripts[key]
		if !ok {
			script = "cat"
		}
		return runtimetest.Spec(s.ID, t.TempDir(), script)
	}
}

func newHost(t *testing.T) *termhost.Host {
	host := termhost.New(shell.Direct{}, ptyunix.New(), runtimetest.Script{})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	return host
}

func startRun(t *testing.T, be *tuitest.Fake, d Deps) *run {
	t.Helper()
	r := &run{t: t, m: New(d), be: be, msgs: make(chan tea.Msg, 1024)}
	t.Cleanup(func() { r.m.Shutdown(context.Background()) })
	r.send(tea.WindowSizeMsg{Width: 100, Height: 20})
	r.exec(r.m.Init())
	r.until("projects", func() bool { return !strings.Contains(r.view(), "loading…") })
	return r
}

func (r *run) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				r.exec(c)
			}
		default:
			r.msgs <- msg
		}
	}()
}

func (r *run) send(msg tea.Msg) {
	next, cmd := r.m.Update(msg)
	r.m = next.(Model)
	r.exec(cmd)
}

func (r *run) press(keys ...string) {
	for _, k := range keys {
		r.send(keyMsg(k))
	}
}

func (r *run) typeText(s string) {
	for _, c := range s {
		r.send(tea.KeyPressMsg{Code: c, Text: string(c)})
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "prefix":
		return tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func (r *run) until(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case msg := <-r.msgs:
			if _, ok := msg.(tea.QuitMsg); ok {
				r.quit = true
				continue
			}
			r.send(msg)
		case <-deadline:
			r.t.Fatalf("timed out waiting for %s; view:\n%s", what, r.view())
		}
	}
}

func (r *run) view() string { return ansi.Strip(r.m.View().Content) }

func (r *run) shows(s string) func() bool {
	return func() bool { return strings.Contains(r.view(), s) }
}

func (r *run) settle() {
	r.t.Helper()
	deadline := time.After(100 * time.Millisecond)
	for {
		select {
		case msg := <-r.msgs:
			if _, ok := msg.(tea.QuitMsg); ok {
				r.quit = true
				continue
			}
			r.send(msg)
		case <-deadline:
			return
		}
	}
}

// depsOf serves every port from be; sessions run here on host, as /bin/sh
// scripts in place of claude.
func depsOf(be *tuitest.Fake, host ports.TerminalHost) Deps {
	return Deps{
		Projects: be, Repositories: be, Board: be, Agents: be,
		Sessions: be, Interactive: be, Terminals: be, Feed: be,
		RunsOn: domain.RunnerTUI, Host: host, RunnerHost: "laptop",
	}
}

func fixture() *tuitest.Fake {
	return &tuitest.Fake{
		Projects:     []*domain.Project{{ID: "p1", Name: "api", RootDir: "/src/api"}, {ID: "p2", Name: "web", RootDir: "/src/web"}},
		Repositories: []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "api"}},
		Tickets: []*domain.Ticket{
			{ID: "t-done", ProjectID: "p1", Title: "Old work", Status: domain.TicketStatusDone},
			{ID: "t1", ProjectID: "p1", Title: "Fix flaky tests", Status: domain.TicketStatusInProgress, Description: "they flake"},
		},
		Agents: []*domain.Agent{{ID: "a1", Name: "reviewer", Description: "reviews code"}},
	}
}

// depth is how many screens are stacked: 1 projects, 2 tasks, 3 a task.
func (r *run) depth() int { return len(r.m.stack) }

// openTask goes projects → api → its first task (the in-progress one).
func (r *run) openTask() {
	r.t.Helper()
	r.press("enter")
	r.until("tasks", r.shows("Fix flaky tests"))
	r.press("enter")
	r.until("task screen", r.shows("No agent sessions open"))
}

// startSession opens the picker, keeps plain claude, and starts with prompt.
func (r *run) startSession(prompt string) {
	r.t.Helper()
	r.press("n")
	r.until("picker", r.shows("plain claude"))
	r.press("enter")
	r.until("prompt step", r.shows("First message"))
	r.typeText(prompt)
	r.press("enter")
}

func TestClient_NavigatesProjectsTasksAndBack(t *testing.T) {
	r := newRun(t, fixture(), nil)
	if v := r.view(); !strings.Contains(v, "api") || !strings.Contains(v, "web") {
		t.Fatalf("projects not listed:\n%s", v)
	}
	r.press("enter")
	r.until("tasks", r.shows("Fix flaky tests"))

	v := r.view()
	if strings.Index(v, "Fix flaky tests") > strings.Index(v, "Old work") {
		t.Errorf("in-progress task not listed before the done one:\n%s", v)
	}
	if !strings.Contains(v, "coding pool › api") {
		t.Errorf("breadcrumb missing:\n%s", v)
	}

	r.press("enter")
	r.until("task", r.shows("No agent sessions open"))
	if v := r.view(); !strings.Contains(v, "api › Fix flaky tests") || !strings.Contains(v, "they flake") {
		t.Errorf("task screen lacks breadcrumb or description:\n%s", v)
	}
	r.press("esc")
	r.until("back to tasks", func() bool { return r.depth() == 2 })
	r.press("esc")
	r.until("back to projects", func() bool { return r.depth() == 1 })
	r.press("q")
	r.until("quit", func() bool { return r.quit })
}

func TestClient_StartsASessionAsAPane(t *testing.T) {
	be := fixture()
	r := newRun(t, be, map[string]string{"hi": `printf 'agent up\r\n'; cat`})
	r.openTask()
	r.startSession("hi")
	r.until("pane output", r.shows("agent up"))

	s, ok := be.Session("s1")
	if !ok || s.TicketID != "t1" || s.RepositoryID != "r1" || s.AgentID != "" || s.Task != "hi" {
		t.Fatalf("session not started for the task with plain claude: %+v", s)
	}
	if tabs := strings.SplitN(r.view(), "\n", 3)[1]; !strings.Contains(tabs, " 1 claude ") {
		t.Errorf("tab not labelled claude: %q", tabs)
	}

	// Keys go to the pane.
	r.typeText("echo-me")
	r.press("enter")
	r.until("echo", r.shows("echo-me"))
}

func TestClient_AgentPickerAndRepositoryStep(t *testing.T) {
	be := fixture()
	be.Repositories = append(be.Repositories, &domain.Repository{ID: "r2", ProjectID: "p1", Name: "worker"})
	r := newRun(t, be, nil)
	r.openTask()

	r.press("n")
	r.until("picker", r.shows("reviewer"))
	r.press("down", "enter")
	r.until("repository step", r.shows("worker"))
	r.press("down", "enter")
	r.until("prompt step", r.shows("First message"))
	r.press("enter")
	r.until("session", func() bool { _, ok := be.Session("s1"); return ok })

	s, _ := be.Session("s1")
	if s.AgentID != "a1" || s.RepositoryID != "r2" {
		t.Fatalf("picked reviewer on worker, got agent %q repo %q", s.AgentID, s.RepositoryID)
	}
	r.until("pane labelled reviewer", r.shows(" 1 reviewer "))
}

func TestClient_PanesSurviveLeavingTheTask(t *testing.T) {
	r := newRun(t, fixture(), map[string]string{"one": `printf 'first agent\r\n'; cat`})
	r.openTask()
	r.startSession("one")
	r.until("pane", r.shows("first agent"))

	r.press("prefix", "b")
	r.until("tasks", func() bool { return r.depth() == 2 })
	if !strings.Contains(r.view(), "● 1 live") {
		t.Errorf("task list lacks the live count:\n%s", r.view())
	}
	r.press("esc")
	r.until("projects", func() bool { return r.depth() == 1 })
	if !strings.Contains(r.view(), "● 1 live") {
		t.Errorf("project list lacks the live count:\n%s", r.view())
	}

	r.press("enter")
	r.until("tasks", r.shows("Fix flaky tests"))
	r.press("enter")
	r.until("pane still there", r.shows("first agent"))
	if len(r.be.EndCalls()) != 0 {
		t.Errorf("leaving the task ended sessions: %+v", r.be.EndCalls())
	}
}

func TestClient_ExitEndsTheSessionWithItsCode(t *testing.T) {
	be := fixture()
	r := newRun(t, be, map[string]string{"crash": `printf 'bye\r\n'; exit 3`})
	r.openTask()
	r.startSession("crash")
	r.until("end reported", func() bool { return len(be.EndCalls()) > 0 })

	if got := be.EndCalls(); !slices.Equal(got, []tuitest.EndCall{{SessionID: "s1", ExitCode: 3}}) {
		t.Fatalf("EndSession calls = %+v, want s1 exit 3", got)
	}
	if s, _ := be.Session("s1"); s.Status != domain.SessionFailed {
		t.Errorf("status = %s, want failed", s.Status)
	}
	r.until("exit mark", r.shows(" 1 claude ✗ "))
}

func TestClient_CloseEndsTheSessionAsStopped(t *testing.T) {
	be := fixture()
	r := newRun(t, be, map[string]string{"x": `printf 'running\r\n'; cat`})
	r.openTask()
	r.startSession("x")
	r.until("pane", r.shows("running"))

	r.press("prefix", "x")
	r.until("end reported", func() bool { return len(be.EndCalls()) > 0 })
	r.settle()

	if got := be.EndCalls(); !slices.Equal(got, []tuitest.EndCall{{SessionID: "s1", Closed: true}}) {
		t.Fatalf("EndSession calls = %+v, want exactly one closed end for s1", got)
	}
	if s, _ := be.Session("s1"); s.Status != domain.SessionStopped {
		t.Errorf("status = %s, want stopped", s.Status)
	}
	r.until("empty task", r.shows("No agent sessions open"))
}

func TestClient_SessionsOverlayResumesAnEndedSession(t *testing.T) {
	be := fixture()
	be.Sessions = []*domain.Session{
		{ID: "old", ProjectID: "p1", TicketID: "t1", Branch: "agent/fix-flaky-tests-1a2b", Status: domain.SessionStopped, Interactive: true, ClaudeSessionID: "old"},
		{ID: "bot", ProjectID: "p1", TicketID: "t1", Branch: "agent/headless", Status: domain.SessionDone},
	}
	r := newRun(t, be, map[string]string{"resume": `printf 'welcome back\r\n'; cat`})
	r.press("enter")
	r.until("tasks", r.shows("2 sessions"))
	r.press("enter")
	r.until("task", r.shows("No agent sessions open"))

	r.press("s")
	r.until("overlay", r.shows("fix-flaky-tests-1a2b"))
	if !strings.Contains(r.view(), "headless") {
		t.Errorf("headless session not listed:\n%s", r.view())
	}

	r.press("down", "enter")
	r.until("headless explained", r.shows("web UI"))
	r.press("g", "enter")
	r.until("resumed pane", r.shows("welcome back"))
	if s, _ := be.Session("old"); s.Status != domain.SessionRunning {
		t.Errorf("status after resume = %s, want running", s.Status)
	}
	if !strings.Contains(r.view(), " 1 fix-flaky-tests-1a2b ") {
		t.Errorf("resumed pane not labelled by its branch:\n%s", r.view())
	}
}

func TestClient_ShowsAPIErrorsByCode(t *testing.T) {
	be := fixture()
	be.Err = map[string]error{"StartInteractive": &domain.StructuredError{Code: "BRANCH_EXISTS", Message: "branch already exists"}}
	r := newRun(t, be, nil)
	r.openTask()
	r.startSession("x")
	r.until("error", r.shows("a branch for this session already exists"))
	if r.m.panes.Deck("t1").Len() != 0 {
		t.Error("a failed start opened a pane")
	}
}

func TestClient_ShutdownEndsOpenSessions(t *testing.T) {
	be := fixture()
	r := newRun(t, be, nil)
	r.openTask()
	r.startSession("a")
	r.until("pane", func() bool { return r.m.panes.Deck("t1").Len() == 1 })

	r.m.Shutdown(context.Background())
	if got := be.EndCalls(); !slices.Equal(got, []tuitest.EndCall{{SessionID: "s1", Closed: true}}) {
		t.Fatalf("EndSession calls = %+v, want s1 closed", got)
	}
}

func TestClient_LaunchFinishingAfterLeavingTheTaskStillOpensAPane(t *testing.T) {
	be := fixture()
	r := newRun(t, be, map[string]string{"late": `printf 'late agent\r\n'; cat`})
	r.openTask()
	r.startSession("late")
	// Leave before the launch comes back: the task screen is gone by the time
	// its session starts, so only the registry can open the pane.
	r.press("esc")
	r.until("session started", func() bool { _, ok := be.Session("s1"); return ok })
	r.until("pane in the background", func() bool { return r.m.panes.LiveIn("t1") == 1 })
	r.until("live count", r.shows("● 1 live"))

	r.press("enter")
	r.until("pane shown on return", r.shows("late agent"))
}

func TestClient_StackAndQuitKeys(t *testing.T) {
	r := newRun(t, fixture(), nil)
	r.press("esc")
	r.settle()
	if r.depth() != 1 {
		t.Fatalf("esc on the root screen changed the stack to %d screens", r.depth())
	}

	r.openTask()
	r.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	r.settle()
	if r.quit {
		t.Fatal("ctrl+c on the task screen quit; it belongs to claude there")
	}

	r.press("esc")
	r.until("tasks", func() bool { return r.depth() == 2 })
	r.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	r.until("quit", func() bool { return r.quit })
}

func TestClient_SessionsRecordWhereTheyRun(t *testing.T) {
	be := fixture()
	r := newRun(t, be, map[string]string{"here": `printf 'local\r\n'; cat`})
	r.openTask()
	r.startSession("here")
	r.until("pane", r.shows("local"))
	if s, _ := be.Session("s1"); s.RunsOn != domain.RunnerTUI || s.RunnerHost != "laptop" {
		t.Fatalf("session runs on %q at %q, want tui on laptop", s.RunsOn, s.RunnerHost)
	}
}

func TestClient_ServerRunsTheSessionAndRecordsItsEnd(t *testing.T) {
	be := fixture()
	r := newServerRun(t, be, map[string]string{"up": `printf 'on the server\r\n'; read x; exit 2`})
	r.openTask()
	r.startSession("up")
	r.until("pane attached to the server's terminal", r.shows("on the server"))
	if s, _ := be.Session("s1"); s.RunsOn != domain.RunnerServer {
		t.Fatalf("session runs on %q, want server", s.RunsOn)
	}

	r.press("enter") // the script reads a line, then exits 2
	r.until("server recorded the failure", func() bool { s, _ := be.Session("s1"); return s.Status == domain.SessionFailed })
	r.until("exit mark", r.shows(" 1 claude ✗ "))
	if calls := be.EndCalls(); len(calls) != 0 {
		t.Errorf("client ended a session the server runs: %+v", calls)
	}
}

func TestClient_ClosingAServerSessionStopsItOnTheServer(t *testing.T) {
	be := fixture()
	r := newServerRun(t, be, map[string]string{"x": `printf 'running\r\n'; cat`})
	r.openTask()
	r.startSession("x")
	r.until("pane", r.shows("running"))

	r.press("prefix", "x")
	r.until("stopped", func() bool { s, _ := be.Session("s1"); return s.Status == domain.SessionStopped })
	r.until("empty task", r.shows("No agent sessions open"))
	if calls := be.EndCalls(); len(calls) != 0 {
		t.Errorf("client ended a session the server runs: %+v", calls)
	}
}

func TestClient_QuittingDetachesAndTheNextClientReattaches(t *testing.T) {
	be := fixture()
	first := newServerRun(t, be, map[string]string{"keep": `printf 'still here\r\n'; cat`})
	first.openTask()
	first.startSession("keep")
	first.until("pane", first.shows("still here"))

	first.m.Shutdown(context.Background())
	if s, _ := be.Session("s1"); s.Status != domain.SessionRunning {
		t.Fatalf("quitting the client ended the server's session: %s", s.Status)
	}
	term, err := be.Server.Attach("s1")
	if err != nil {
		t.Fatalf("the process died with the client: %v", err)
	}
	select {
	case <-term.Done():
		t.Fatal("the process died with the client")
	default:
	}

	second := newServerRun(t, be, nil)
	second.until("reattached in the background", func() bool { return second.m.panes.LiveIn("t1") == 1 })
	second.until("live count", second.shows("● 1 live"))
	second.press("enter")
	second.until("tasks", second.shows("Fix flaky tests"))
	second.press("enter")
	second.until("the screen it left", second.shows("still here"))

	// Typing reaches the same process.
	second.typeText("hello again")
	second.press("enter")
	second.until("echo", second.shows("hello again"))
}

// Another client starts and ends a session on a task: the lists here follow
// without a refresh.
func TestClient_ListsFollowAnotherClient(t *testing.T) {
	be := fixture()
	r := newRun(t, be, nil)
	r.press("enter")
	r.until("tasks", r.shows("Fix flaky tests"))
	r.until("following the project", func() bool { return be.Following("p1") == 1 })

	elsewhere := &domain.Session{ID: "x1", ProjectID: "p1", TicketID: "t1", Branch: "agent/elsewhere", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerTUI, RunnerHost: "desktop"}
	be.Put(elsewhere)
	r.until("count without a refresh", r.shows("1 session"))

	r.press("enter")
	r.until("task", r.shows("No agent sessions open"))
	r.press("s")
	r.until("overlay lists it", r.shows("agent/elsewhere"))
	r.until("as running", r.shows("running"))

	done := *elsewhere
	done.Status = domain.SessionDone
	be.Put(&done)
	r.until("overlay shows it ended", r.shows("done"))
}

func TestClient_FollowsOnlyTheProjectYouAreIn(t *testing.T) {
	be := fixture()
	r := newRun(t, be, nil)
	r.settle()
	if n := be.Following("p1"); n != 0 {
		t.Fatalf("following p1 from the project list: %d", n)
	}
	r.press("enter")
	r.until("following p1", func() bool { return be.Following("p1") == 1 })
	r.until("tasks", r.shows("Fix flaky tests"))
	r.press("enter") // into a task: still p1, still one follow
	r.until("task", r.shows("No agent sessions open"))
	r.settle()
	if n := be.Following("p1"); n != 1 {
		t.Fatalf("follows of p1 inside a task = %d, want 1", n)
	}
	r.press("esc", "esc")
	r.until("stopped following", func() bool { return be.Following("p1") == 0 })
}

// A lost stream says so, comes back on its own, and reloads what it missed.
func TestClient_ReconnectsAndReloadsWhatItMissed(t *testing.T) {
	be := fixture()
	r := newRun(t, be, nil)
	r.press("enter")
	r.until("tasks", r.shows("Fix flaky tests"))
	r.until("following", func() bool { return be.Following("p1") == 1 })

	be.DropFollowers("p1")
	r.until("paused note", r.shows("live updates paused"))
	// Started while nobody followed: only a reload can show it.
	be.Put(&domain.Session{ID: "x1", ProjectID: "p1", TicketID: "t1", Status: domain.SessionRunning, Interactive: true})

	r.until("reconnected", func() bool { return be.Following("p1") == 1 })
	r.until("reloaded", r.shows("1 session"))
	r.until("note gone", func() bool { return !strings.Contains(r.view(), "live updates paused") })
}
