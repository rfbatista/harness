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

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// run drives the client the way the Bubble Tea runtime would: every
// returned command runs on its own goroutine and its message is fed back.
type run struct {
	t    *testing.T
	m    Model
	be   *Fake
	msgs chan tea.Msg
	quit bool
}

// scripts maps a session's first message (or "resume") to the shell script
// its pane runs in place of claude.
func newRun(t *testing.T, be *Fake, scripts map[string]string) *run {
	t.Helper()
	be.Launch = func(s domain.Session, resume bool) ports.Launch {
		key := s.Task
		if resume {
			key = "resume"
		}
		script, ok := scripts[key]
		if !ok {
			script = "cat"
		}
		return ports.Launch{SessionID: s.ID, Args: []string{"-c", script}, Dir: t.TempDir()}
	}
	r := &run{t: t, m: New(be, "/bin/sh"), be: be, msgs: make(chan tea.Msg, 1024)}
	t.Cleanup(func() {
		for _, d := range r.m.decks {
			d.CloseAll()
		}
	})
	r.send(tea.WindowSizeMsg{Width: 100, Height: 20})
	r.exec(r.m.Init())
	r.until("projects", func() bool { return len(r.m.projects) > 0 })
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

func fixture() *Fake {
	return &Fake{
		Projects:     []domain.Project{{ID: "p1", Name: "api", RootDir: "/src/api"}, {ID: "p2", Name: "web", RootDir: "/src/web"}},
		Repositories: []domain.Repository{{ID: "r1", ProjectID: "p1", Name: "api"}},
		Tickets: []domain.Ticket{
			{ID: "t-done", ProjectID: "p1", Title: "Old work", Status: domain.TicketStatusDone},
			{ID: "t1", ProjectID: "p1", Title: "Fix flaky tests", Status: domain.TicketStatusInProgress, Description: "they flake"},
		},
		Agents: []Agent{{ID: "a1", Name: "reviewer", Description: "reviews code"}},
	}
}

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

func TestWorkbench_NavigatesProjectsTasksAndBack(t *testing.T) {
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
	if r.m.screen != screenTasks {
		t.Fatalf("esc from task: screen %d, want tasks", r.m.screen)
	}
	r.press("esc")
	if r.m.screen != screenProjects {
		t.Fatalf("esc from tasks: screen %d, want projects", r.m.screen)
	}
	r.press("q")
	r.until("quit", func() bool { return r.quit })
}

func TestWorkbench_StartsASessionAsAPane(t *testing.T) {
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

func TestWorkbench_AgentPickerAndRepositoryStep(t *testing.T) {
	be := fixture()
	be.Repositories = append(be.Repositories, domain.Repository{ID: "r2", ProjectID: "p1", Name: "worker"})
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

func TestWorkbench_PanesSurviveLeavingTheTask(t *testing.T) {
	r := newRun(t, fixture(), map[string]string{"one": `printf 'first agent\r\n'; cat`})
	r.openTask()
	r.startSession("one")
	r.until("pane", r.shows("first agent"))

	r.press("prefix", "b")
	r.until("tasks", func() bool { return r.m.screen == screenTasks })
	if !strings.Contains(r.view(), "● 1 live") {
		t.Errorf("task list lacks the live count:\n%s", r.view())
	}
	r.press("esc")
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

func TestWorkbench_ExitEndsTheSessionWithItsCode(t *testing.T) {
	be := fixture()
	r := newRun(t, be, map[string]string{"crash": `printf 'bye\r\n'; exit 3`})
	r.openTask()
	r.startSession("crash")
	r.until("end reported", func() bool { return len(be.EndCalls()) > 0 })

	if got := be.EndCalls(); !slices.Equal(got, []EndCall{{SessionID: "s1", ExitCode: 3}}) {
		t.Fatalf("EndSession calls = %+v, want s1 exit 3", got)
	}
	if s, _ := be.Session("s1"); s.Status != domain.SessionFailed {
		t.Errorf("status = %s, want failed", s.Status)
	}
	r.until("exit mark", r.shows(" 1 claude ✗ "))
}

func TestWorkbench_CloseEndsTheSessionAsStopped(t *testing.T) {
	be := fixture()
	r := newRun(t, be, map[string]string{"x": `printf 'running\r\n'; cat`})
	r.openTask()
	r.startSession("x")
	r.until("pane", r.shows("running"))

	r.press("prefix", "x")
	r.until("end reported", func() bool { return len(be.EndCalls()) > 0 })
	r.settle()

	if got := be.EndCalls(); !slices.Equal(got, []EndCall{{SessionID: "s1", Closed: true}}) {
		t.Fatalf("EndSession calls = %+v, want exactly one closed end for s1", got)
	}
	if s, _ := be.Session("s1"); s.Status != domain.SessionStopped {
		t.Errorf("status = %s, want stopped", s.Status)
	}
	r.until("empty task", r.shows("No agent sessions open"))
}

func TestWorkbench_SessionsOverlayResumesAnEndedSession(t *testing.T) {
	be := fixture()
	be.Sessions = []domain.Session{
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
	if !strings.Contains(r.view(), "web UI") {
		t.Errorf("opening a headless session did not explain itself:\n%s", r.view())
	}
	r.press("g", "enter")
	r.until("resumed pane", r.shows("welcome back"))
	if s, _ := be.Session("old"); s.Status != domain.SessionRunning {
		t.Errorf("status after resume = %s, want running", s.Status)
	}
	if !strings.Contains(r.view(), " 1 fix-flaky-tests-1a2b ") {
		t.Errorf("resumed pane not labelled by its branch:\n%s", r.view())
	}
}

func TestWorkbench_ShowsAPIErrorsByCode(t *testing.T) {
	be := fixture()
	be.Err = map[string]error{"StartSession": &APIError{Status: 409, Code: "BRANCH_EXISTS", Message: "branch already exists"}}
	r := newRun(t, be, nil)
	r.openTask()
	r.startSession("x")
	r.until("error", r.shows("a branch for this session already exists"))
	if r.m.deck().Len() != 0 {
		t.Error("a failed start opened a pane")
	}
}

func TestWorkbench_ShutdownEndsOpenSessions(t *testing.T) {
	be := fixture()
	r := newRun(t, be, nil)
	r.openTask()
	r.startSession("a")
	r.until("pane", func() bool { return r.m.deck().Len() == 1 })

	r.m.Shutdown(context.Background())
	if got := be.EndCalls(); !slices.Equal(got, []EndCall{{SessionID: "s1", Closed: true}}) {
		t.Fatalf("EndSession calls = %+v, want s1 closed", got)
	}
}
