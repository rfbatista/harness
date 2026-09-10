package spawn

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
	"operators-mcp/internal/domain"
)

func fixture() *backend.Fake {
	f := backend.NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "alpha"}, {ID: "p2", Name: "beta"}}
	f.Repositories = []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "alpha-repo"}, {ID: "r2", ProjectID: "p2", Name: "beta-repo"}}
	f.Branches = map[string][]domain.GitBranch{"r2": {{Name: "origin/dev", Remote: true}, {Name: "main", IsHead: true}}, "r1": {{Name: "main", IsHead: true}}}
	f.Agents = []*domain.Agent{{ID: "a1", Name: "reviewer"}, {ID: "a2", Name: "builder"}}
	f.Tickets = []*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Fix login redirect"}, {ID: "t2", ProjectID: "p2", Title: "Broken build"}}
	return f
}

func ctx() core.Context { return core.Context{Theme: theme.Dark(), Width: 100, Height: 30} }

func key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) { return m.Update(k) }
func ch(r rune) tea.KeyPressMsg                       { return tea.KeyPressMsg{Code: r, Text: string(r)} }
func enter() tea.KeyPressMsg                          { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func esc() tea.KeyPressMsg                            { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func tab() tea.KeyPressMsg                            { return tea.KeyPressMsg{Code: tea.KeyTab} }

func typeKeys(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(ch(r))
	}
	return m
}

// run executes a command and feeds its messages back until quiet, returning
// the messages meant for the owner (DoneMsg, CancelMsg, toasts).
func run(m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case DoneMsg, CancelMsg, core.ToastMsg, core.RefreshMsg:
			out = append(out, msg)
		case nil:
		default:
			var next tea.Cmd
			m, next = m.Update(msg)
			queue = append(queue, next)
		}
	}
	return m, out
}

func press(m Model, k tea.KeyPressMsg) (Model, []tea.Msg) {
	m, cmd := m.Update(k)
	return run(m, cmd)
}

func TestWizardWalksEveryStepAndStartsSession(t *testing.T) {
	f := fixture()
	m := New(ctx(), f, backend.Load(f), Options{})
	if !strings.Contains(m.View(), "alpha") || !strings.Contains(m.View(), "beta") {
		t.Fatalf("project step:\n%s", m.View())
	}
	m, _ = press(m, ch('j'))
	m, _ = press(m, enter()) // beta
	if !strings.Contains(m.View(), "beta-repo") || strings.Contains(m.View(), "alpha-repo") {
		t.Fatalf("repository step should list beta's repos:\n%s", m.View())
	}
	m, _ = press(m, enter()) // beta-repo → branches load
	v := m.View()
	if !strings.Contains(v, "main") || !strings.Contains(v, "origin/dev") {
		t.Fatalf("branch step:\n%s", v)
	}
	if m.Step() != StepBranch || !strings.Contains(v, "HEAD") {
		t.Fatalf("branch step should mark HEAD and preselect it: step=%v\n%s", m.Step(), v)
	}
	m, _ = press(m, enter()) // main (HEAD preselected)
	if !strings.Contains(m.View(), "reviewer") || !strings.Contains(m.View(), "builder") {
		t.Fatalf("agent step:\n%s", m.View())
	}
	m, _ = press(m, ch('j'))
	m, _ = press(m, enter()) // builder
	v = m.View()
	if !strings.Contains(v, "New task") || !strings.Contains(v, "Broken build") || strings.Contains(v, "Fix login") {
		t.Fatalf("task step lists beta tickets plus New task:\n%s", v)
	}
	m, _ = press(m, enter()) // New task (first row)
	v = m.View()
	if !strings.Contains(v, "Title") || !strings.Contains(v, "Instruction") || !strings.Contains(v, "Branch") {
		t.Fatalf("details form:\n%s", v)
	}
	m = typeKeys(m, "Add caching layer")
	m, _ = press(m, tab()) // goal
	m, _ = m.Update(editorDone("goal", "Cache hot reads."))
	m, _ = press(m, tab()) // instruction
	m = typeKeys(m, "Implement the cache")
	if !regexp.MustCompile(`add-caching-layer-[0-9a-f]{4}`).MatchString(plain(m.View())) {
		t.Fatalf("branch should be suggested from the title:\n%s", m.View())
	}
	m, out := press(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if len(f.Started) != 1 {
		t.Fatalf("session not started: %+v out=%v\n%s", f.Started, out, m.View())
	}
	req := f.Started[0]
	if req.ProjectID != "p2" || req.RepositoryID != "r2" || req.AgentID != "a2" || req.BaseBranch != "main" || req.Task != "Implement the cache" {
		t.Fatalf("request: %+v", req)
	}
	if len(f.Tickets) != 3 || f.Tickets[2].Title != "Add caching layer" || f.Tickets[2].Description != "Cache hot reads." || req.TicketID != f.Tickets[2].ID {
		t.Fatalf("new ticket should be created and linked: %+v req=%+v", f.Tickets, req)
	}
	var done *DoneMsg
	for _, o := range out {
		if d, ok := o.(DoneMsg); ok {
			done = &d
		}
	}
	if done == nil || done.Session == nil || done.Session.ID == "" {
		t.Fatalf("DoneMsg with the session expected, got %v", out)
	}
}

func TestWizardPrefilledTicketSkipsProjectAndTaskSteps(t *testing.T) {
	f := fixture()
	m := New(ctx(), f, backend.Load(f), Options{ProjectID: "p1", TicketID: "t1"})
	if m.Step() != StepRepository {
		t.Fatalf("should start at repository, got %v", m.Step())
	}
	m, _ = press(m, enter()) // alpha-repo
	m, _ = press(m, enter()) // main
	m, _ = press(m, enter()) // reviewer
	if m.Step() != StepDetails {
		t.Fatalf("task step should be skipped, got %v", m.Step())
	}
	if !regexp.MustCompile(`fix-login-redirect-[0-9a-f]{4}`).MatchString(plain(m.View())) || strings.Contains(m.View(), "Title") {
		t.Fatalf("details for an existing task: branch from its title, no title field:\n%s", m.View())
	}
	m = typeKeys(m, "Make the redirect stop looping")
	_, out := press(m, enter())
	if len(f.Started) != 1 || f.Started[0].TicketID != "t1" || len(f.Tickets) != 2 {
		t.Fatalf("existing ticket should be reused: %+v", f.Started)
	}
	if len(out) == 0 {
		t.Fatal("DoneMsg expected")
	}
}

func TestWizardSingleProjectAutoSkips(t *testing.T) {
	f := fixture()
	f.Projects = f.Projects[:1]
	m := New(ctx(), f, backend.Load(f), Options{})
	if m.Step() != StepRepository {
		t.Fatalf("one project should skip the project step, got %v", m.Step())
	}
}

func TestWizardBranchExistsStaysOnForm(t *testing.T) {
	f := fixture()
	f.Sessions = []*domain.Session{{ID: "s0", ProjectID: "p1", Branch: "taken"}}
	m := New(ctx(), f, backend.Load(f), Options{ProjectID: "p1", TicketID: "t1"})
	m, _ = press(m, enter())
	m, _ = press(m, enter())
	m, _ = press(m, enter())
	m = typeKeys(m, "do it")
	m, _ = press(m, tab()) // branch field
	m = clearField(m)
	m = typeKeys(m, "taken")
	m, out := press(m, enter())
	if len(out) != 0 || !strings.Contains(m.View(), "already exists") {
		t.Fatalf("BRANCH_EXISTS should stay inline:\n%s", m.View())
	}
}

func TestWizardEscGoesBackThenCancels(t *testing.T) {
	f := fixture()
	m := New(ctx(), f, backend.Load(f), Options{})
	m, _ = press(m, enter()) // alpha
	if m.Step() != StepRepository {
		t.Fatalf("step: %v", m.Step())
	}
	m, _ = press(m, esc())
	if m.Step() != StepProject {
		t.Fatalf("esc should go back, got %v", m.Step())
	}
	_, out := press(m, esc())
	if len(out) != 1 {
		t.Fatalf("esc on the first step should cancel, got %v", out)
	}
	if _, ok := out[0].(CancelMsg); !ok {
		t.Fatalf("want CancelMsg got %#v", out[0])
	}
}

func TestSuggestBranchSlugifies(t *testing.T) {
	got := SuggestBranch("Fix: the Login  redirect!", "ab12")
	if got != "fix-the-login-redirect-ab12" {
		t.Fatalf("slug: %q", got)
	}
	if got := SuggestBranch("", "ab12"); got != "agent-ab12" {
		t.Fatalf("empty title: %q", got)
	}
}
