package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
	"operators-mcp/internal/domain"
)

func fixture() *backend.Fake {
	f := backend.NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "coding_pool"}, {ID: "p2", Name: "other"}}
	f.Repositories = []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "main-repo"}}
	f.Branches = map[string][]domain.GitBranch{"r1": {{Name: "main", IsHead: true}}}
	f.Agents = []*domain.Agent{{ID: "a1", Name: "reviewer"}}
	f.Tickets = []*domain.Ticket{
		{ID: "t1", ProjectID: "p1", Title: "Fix login redirect", Description: "# Goal\n\nStop the **loop**.", Status: domain.TicketStatusInProgress},
		{ID: "t2", ProjectID: "p1", Title: "Write docs", Status: domain.TicketStatusDone},
	}
	f.Sessions = []*domain.Session{
		{ID: "s1", ProjectID: "p1", TicketID: "t1", Status: domain.SessionWaitingApproval, CostUSD: 0.42, Branch: "fix-login-1a2b", RepositoryID: "r1"},
		{ID: "s2", ProjectID: "p1", TicketID: "t2", Status: domain.SessionDone, CostUSD: 1.1},
	}
	f.Documents = []*domain.Document{{ID: "d1", ProjectID: "p1", Title: "Auth spec", Content: "# Auth\n\nTokens expire."}}
	f.TicketDocuments = map[string][]string{"t1": {"d1"}}
	return f
}

func ready(f *backend.Fake) Model {
	m := New(ctx(), rollup.FilterAll, f)
	m, _ = m.Update(core.SnapshotMsg{Snapshot: backend.Load(f)})
	return m
}

func drive(m Model, cmd tea.Cmd) (Model, []tea.Msg) {
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
		case core.RefreshMsg, core.ToastMsg, core.NavigateMsg:
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
	return drive(m, cmd)
}

func ch(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
func enter() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func esc() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func tab() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyTab} }

func typeKeys(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(ch(r))
	}
	return m
}

func hasNav(out []tea.Msg) *core.NavigateMsg {
	for _, o := range out {
		if n, ok := o.(core.NavigateMsg); ok {
			return &n
		}
	}
	return nil
}

func TestNewTaskFormCreatesTicket(t *testing.T) {
	f := fixture()
	m := ready(f)
	m, _ = press(m, ch('n'))
	if !m.Capturing() || !strings.Contains(m.View(), "New task") {
		t.Fatalf("form expected:\n%s", m.View())
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyRight}) // project → other
	m, _ = press(m, tab())
	m = typeKeys(m, "Ship it")
	m, _ = press(m, tab())
	m, _ = m.Update(components.EditorDoneMsg{Tag: "description", Content: "Everything."})
	_, out := press(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if len(f.Tickets) != 3 || f.Tickets[2].ProjectID != "p2" || f.Tickets[2].Title != "Ship it" || f.Tickets[2].Description != "Everything." {
		t.Fatalf("ticket not created: %+v", f.Tickets)
	}
	refreshed := false
	for _, o := range out {
		if _, ok := o.(core.RefreshMsg); ok {
			refreshed = true
		}
	}
	if !refreshed {
		t.Fatal("refresh expected")
	}
}

func TestSpawnKeyOpensWizardForSelectedTask(t *testing.T) {
	f := fixture()
	m := ready(f)
	m, _ = press(m, ch('s'))
	v := m.View()
	if !m.Capturing() || !strings.Contains(v, "Spawn agent") || !strings.Contains(v, "main-repo") {
		t.Fatalf("wizard should open at the repository step for t1:\n%s", v)
	}
	m, _ = press(m, enter()) // repo
	m, _ = press(m, enter()) // branch
	m, _ = press(m, enter()) // agent
	m = typeKeys(m, "go")
	m, out := press(m, enter())
	if len(f.Started) != 1 || f.Started[0].TicketID != "t1" {
		t.Fatalf("spawn not started: %+v", f.Started)
	}
	nav := hasNav(out)
	if nav == nil || nav.SessionID == "" {
		t.Fatalf("should navigate to the new session, got %v", out)
	}
	if m.Capturing() {
		t.Fatal("wizard should close")
	}
}

func TestEnterOpensTaskDetail(t *testing.T) {
	f := fixture()
	m := ready(f)
	m, _ = press(m, enter())
	v := plain(m.View())
	for _, want := range []string{"Fix login redirect", "Stop the loop", "Auth spec", "fix-login-1a2b", "Needs review", "$0.42"} {
		if !strings.Contains(v, want) {
			t.Fatalf("detail missing %q:\n%s", want, v)
		}
	}
	m, _ = press(m, esc())
	if strings.Contains(plain(m.View()), "Auth spec") {
		t.Fatal("esc should close the detail")
	}
}

func TestTaskDetailOpensDocumentReader(t *testing.T) {
	f := fixture()
	m := ready(f)
	m, _ = press(m, enter())
	m, _ = press(m, enter()) // documents list is focused first; open Auth spec
	if !strings.Contains(plain(m.View()), "Tokens expire") {
		t.Fatalf("reader expected:\n%s", plain(m.View()))
	}
	m, _ = press(m, esc())
	if strings.Contains(plain(m.View()), "Tokens expire") {
		t.Fatal("esc should close the reader")
	}
}

func TestTaskDetailTabToAgentsAndEnterNavigates(t *testing.T) {
	f := fixture()
	m := ready(f)
	m, _ = press(m, enter())
	m, _ = press(m, tab())
	_, out := press(m, enter())
	nav := hasNav(out)
	if nav == nil || nav.SessionID != "s1" {
		t.Fatalf("enter on an agent should navigate to its session, got %v", out)
	}
}

func TestTaskDetailEditAndStatusAndDelete(t *testing.T) {
	f := fixture()
	m := ready(f)
	m, _ = press(m, enter())
	m, _ = press(m, ch('e'))
	m = typeKeys(m, "!")
	m, _ = press(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if f.Tickets[0].Title != "Fix login redirect!" {
		t.Fatalf("title not updated: %+v", f.Tickets[0])
	}
	m, _ = press(m, ch('x'))
	if !strings.Contains(m.View(), "review") {
		t.Fatalf("status picker:\n%s", m.View())
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyRight}) // in_progress → review
	m, _ = press(m, enter())
	if f.Tickets[0].Status != domain.TicketStatusReview {
		t.Fatalf("status not updated: %+v", f.Tickets[0])
	}
	m, _ = press(m, ch('D'))
	m, _ = press(m, ch('y'))
	if len(f.Tickets) != 1 || f.Tickets[0].ID != "t2" {
		t.Fatalf("ticket not deleted: %+v", f.Tickets)
	}
	m, _ = m.Update(core.SnapshotMsg{Snapshot: backend.Load(f)})
	if strings.Contains(plain(m.View()), "Auth spec") {
		t.Fatal("detail should close once its ticket is gone")
	}
}
