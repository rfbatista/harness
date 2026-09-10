package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
	"operators-mcp/internal/domain"
)

func press(m Model, r rune) (Model, tea.Cmd) {
	next, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	return next.(Model), cmd
}

func key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(Model), cmd
}

func seeded() *backend.Fake {
	f := backend.NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "coding_pool", RootDir: "/src/cp"}}
	f.Tickets = []*domain.Ticket{
		{ID: "t1", ProjectID: "p1", Title: "Fix login redirect", Status: domain.TicketStatusInProgress},
		{ID: "t2", ProjectID: "p1", Title: "Write docs", Status: domain.TicketStatusDone},
	}
	f.Sessions = []*domain.Session{
		{ID: "s1", ProjectID: "p1", TicketID: "t1", Status: domain.SessionWaitingApproval, CostUSD: 0.42},
		{ID: "s2", ProjectID: "p1", TicketID: "t2", Status: domain.SessionDone, CostUSD: 1.1},
	}
	f.Agents = []*domain.Agent{{ID: "a1", Name: "reviewer", SkillIDs: []string{"k1"}}}
	f.Skills = []*domain.Skill{{ID: "k1", Name: "tdd-workflow"}}
	f.MCPServers = []*domain.MCPServer{{ID: "m1", Name: "filesystem", Transport: domain.MCPTransportStdio}}
	return f
}

func ready(t *testing.T, f *backend.Fake) Model {
	t.Helper()
	m := New(Options{Backend: f, Theme: theme.Dark(), LogPath: "/dev/null", RefreshInterval: time.Millisecond})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	next, _ = m.Update(core.SnapshotMsg{Snapshot: backend.Load(f)})
	return next.(Model)
}

func TestInitRequestsFirstSnapshot(t *testing.T) {
	m := New(Options{Backend: seeded(), Theme: theme.Dark()})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should schedule the first load")
	}
}

func TestNumberKeysSwitchSections(t *testing.T) {
	m := ready(t, seeded())
	for r, want := range map[rune]core.Section{'1': core.SectionTasks, '4': core.SectionAgents, '5': core.SectionSkills, '6': core.SectionMCPs, '7': core.SectionSettings} {
		m, _ = press(m, r)
		if m.Section() != want {
			t.Fatalf("key %c: got %v want %v", r, m.Section(), want)
		}
	}
}

func TestTabCyclesSectionsAndShiftTabGoesBack(t *testing.T) {
	m := ready(t, seeded())
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.Section() != core.SectionInbox {
		t.Fatalf("tab: got %v", m.Section())
	}
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.Section() != core.SectionTasks {
		t.Fatalf("shift+tab: got %v", m.Section())
	}
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.Section() != core.SectionSettings {
		t.Fatalf("shift+tab wraps: got %v", m.Section())
	}
}

func TestQuitWithoutLiveSessionsQuitsImmediately(t *testing.T) {
	f := seeded()
	f.Sessions = nil
	m := ready(t, f)
	_, cmd := press(m, 'q')
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("want QuitMsg, got %T", cmd())
	}
}

func TestQuitWithLiveSessionsAsksFirst(t *testing.T) {
	f := seeded()
	f.Sessions = []*domain.Session{{ID: "s1", ProjectID: "p1", Status: domain.SessionRunning}}
	m := ready(t, f)
	m, cmd := press(m, 'q')
	if cmd != nil {
		t.Fatal("q with live sessions must not quit outright")
	}
	if !strings.Contains(m.View().Content, "still running") {
		t.Fatalf("expected a confirmation, got:\n%s", m.View().Content)
	}
	_, cmd = press(m, 'y')
	if cmd == nil {
		t.Fatal("y should confirm the quit")
	}
}

func TestTickReloadsSnapshotFromBackend(t *testing.T) {
	f := seeded()
	m := ready(t, f)
	f.Agents = append(f.Agents, &domain.Agent{ID: "a2", Name: "planner"})
	next, cmd := m.Update(core.TickMsg{})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("tick should trigger a load and the next tick")
	}
	msg := runBatch(cmd)
	var snap core.SnapshotMsg
	for _, x := range msg {
		if s, ok := x.(core.SnapshotMsg); ok {
			snap = s
		}
	}
	if len(snap.Snapshot.Agents) != 2 {
		t.Fatalf("snapshot not reloaded: %+v", snap.Snapshot.Agents)
	}
}

func TestViewShowsNavWithCounts(t *testing.T) {
	m := ready(t, seeded())
	v := m.View().Content
	for _, want := range []string{"Tasks", "Inbox", "History", "Agents", "Skills", "MCP", "Settings"} {
		if !strings.Contains(v, want) {
			t.Fatalf("nav missing %q:\n%s", want, v)
		}
	}
	// one waiting approval → inbox badge 1; one done → history 1
	if !strings.Contains(v, "Inbox 1") || !strings.Contains(v, "History 1") {
		t.Fatalf("nav badges missing:\n%s", v)
	}
}

func TestHelpToggleShowsVerboseHints(t *testing.T) {
	m := ready(t, seeded())
	before := m.View().Content
	m, _ = press(m, '?')
	after := m.View().Content
	if len(after) <= len(before) || !strings.Contains(after, "quit") {
		t.Fatalf("? should expand the hint bar:\n%s", after)
	}
}

func TestCtrlKOpensPaletteAndEnterNavigates(t *testing.T) {
	m := ready(t, seeded())
	m, _ = key(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if !strings.Contains(m.View().Content, "Search") {
		t.Fatalf("palette should be visible:\n%s", m.View().Content)
	}
	for _, r := range "filesys" {
		m, _ = press(m, r)
	}
	m, cmd := key(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should choose")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.Section() != core.SectionMCPs {
		t.Fatalf("palette choice should navigate to MCP section, got %v", m.Section())
	}
}

func TestEscClosesPalette(t *testing.T) {
	m := ready(t, seeded())
	m, _ = key(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(m.View().Content, "Search") {
		t.Fatal("esc should close the palette")
	}
}

func TestBackgroundColorMsgSwitchesThemeInAutoMode(t *testing.T) {
	m := New(Options{Backend: seeded(), Theme: theme.Dark(), AutoTheme: true})
	next, _ := m.Update(tea.BackgroundColorMsg{Color: lightColor()})
	m = next.(Model)
	if m.Theme().IsDark {
		t.Fatal("a light terminal background should select the light theme")
	}
}

// runBatch executes a command and flattens one level of tea.BatchMsg.
func runBatch(cmd tea.Cmd) []tea.Msg {
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			if c != nil {
				out = append(out, c())
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestTypingInAFilterDoesNotFireGlobalKeys(t *testing.T) {
	m := ready(t, seeded())
	m, _ = press(m, '5') // skills
	m, _ = press(m, '/')
	m, cmd := press(m, 'q')
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q typed into a filter must not quit")
		}
	}
	m, _ = press(m, '1')
	if m.Section() != core.SectionSkills {
		t.Fatal("digits typed into a filter must not switch sections")
	}
	if !strings.Contains(m.View().Content, "q1") {
		t.Fatalf("filter should contain the typed text:\n%s", m.View().Content)
	}
}
