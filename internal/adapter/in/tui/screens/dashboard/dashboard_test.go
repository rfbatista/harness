package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
	"operators-mcp/internal/adapter/in/tui/theme"
	"operators-mcp/internal/domain"
)

func snap() backend.Snapshot {
	f := backend.NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "coding_pool"}, {ID: "p2", Name: "other"}}
	f.Tickets = []*domain.Ticket{
		{ID: "t1", ProjectID: "p1", Title: "Fix login redirect"},
		{ID: "t2", ProjectID: "p1", Title: "Write docs"},
		{ID: "t3", ProjectID: "p2", Title: "Broken build"},
	}
	f.Sessions = []*domain.Session{
		{ID: "s1", ProjectID: "p1", TicketID: "t1", Status: domain.SessionWaitingApproval, CostUSD: 0.42},
		{ID: "s2", ProjectID: "p1", TicketID: "t2", Status: domain.SessionDone, CostUSD: 1.1},
		{ID: "s3", ProjectID: "p2", TicketID: "t3", Status: domain.SessionFailed},
	}
	return backend.Load(f)
}

func ctx() core.Context { return core.Context{Theme: theme.Dark(), Width: 100, Height: 30} }

func newWith(pinned rollup.Filter) Model {
	m := New(ctx(), pinned, backend.NewFake())
	m, _ = m.Update(core.SnapshotMsg{Snapshot: snap()})
	return m
}

func TestTasksViewListsEveryTicketWithStatus(t *testing.T) {
	v := newWith(rollup.FilterAll).View()
	for _, want := range []string{"Fix login redirect", "Write docs", "Broken build", "Needs review", "Done", "Blocked"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
}

func TestStatTilesReflectSnapshot(t *testing.T) {
	v := newWith(rollup.FilterAll).View()
	for _, want := range []string{"Needs review", "Blocked", "$1.52"} {
		if !strings.Contains(v, want) {
			t.Fatalf("tiles missing %q in:\n%s", want, v)
		}
	}
}

func TestInboxPinsAttentionFilter(t *testing.T) {
	v := newWith(rollup.FilterAttention).View()
	if strings.Contains(v, "Write docs") || !strings.Contains(v, "Fix login redirect") || !strings.Contains(v, "Broken build") {
		t.Fatalf("inbox should only show attention rows:\n%s", v)
	}
}

func TestHistoryPinsDoneFilter(t *testing.T) {
	v := newWith(rollup.FilterDone).View()
	if !strings.Contains(v, "Write docs") || strings.Contains(v, "Fix login redirect") {
		t.Fatalf("history should only show done rows:\n%s", v)
	}
}

func TestFKeyCyclesStatusFilterOnTasks(t *testing.T) {
	m := newWith(rollup.FilterAll)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if m.Filter() != rollup.FilterInFlight {
		t.Fatalf("first f: %v", m.Filter())
	}
}

func TestPKeyCyclesProjectFilter(t *testing.T) {
	m := newWith(rollup.FilterAll)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	v := m.View()
	if strings.Contains(v, "Broken build") || !strings.Contains(v, "coding_pool") {
		t.Fatalf("project filter should hide p2 rows:\n%s", v)
	}
}

func TestVTogglesKanban(t *testing.T) {
	m := newWith(rollup.FilterAll)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	v := m.View()
	if !strings.Contains(v, "In flight") || !strings.Contains(v, "Broken build") {
		t.Fatalf("kanban should show columns and cards:\n%s", v)
	}
}

func TestJKMoveSelectionAndEnterOpensTask(t *testing.T) {
	m := newWith(rollup.FilterAll)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.Selected() == nil || m.Selected().ID != "t2" {
		t.Fatalf("j should select the second row, got %+v", m.Selected())
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.Capturing() || !strings.Contains(m.View(), "Write docs") || !strings.Contains(m.View(), "Documents on this task") {
		t.Fatalf("enter should open the task detail in place:\n%s", m.View())
	}
}
