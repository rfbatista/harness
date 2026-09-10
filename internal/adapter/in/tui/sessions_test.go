package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/screens/session"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

func withEvents(f *backend.Fake) *backend.Fake {
	f.Sessions[0].Branch = "fix-login-1a2b"
	f.Sessions[0].Task = "Fix the login redirect"
	f.Emit("s1", orchestration.SessionEvent{Type: "user_message", Text: "Fix the login redirect", Status: domain.SessionThinking})
	f.Emit("s1", orchestration.SessionEvent{Type: "output", Text: "Looking at the router."})
	return f
}

func openSession(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(core.NavigateMsg{SessionID: "s1"})
	return next.(Model), cmd
}

func TestNavigateToSessionShowsItAndEscReturns(t *testing.T) {
	m := ready(t, withEvents(seeded()))
	m, _ = press(m, '4') // agents section
	m, cmd := openSession(t, m)
	if cmd == nil {
		t.Fatal("opening a session should start its event wait")
	}
	v := plain(m.View().Content)
	if !strings.Contains(v, "fix-login-1a2b") || !strings.Contains(v, "Looking at the router.") {
		t.Fatalf("session screen expected:\n%s", v)
	}
	// The composer owns the keys: typing letters must not switch sections.
	m, _ = press(m, '1')
	if !strings.Contains(plain(m.View().Content), "Looking at the router.") {
		t.Fatal("digits typed in the composer must stay in the session")
	}
	m, back := key(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if back == nil {
		t.Fatal("esc should emit BackMsg")
	}
	next, _ := m.Update(back())
	m = next.(Model)
	if m.Section() != core.SectionAgents || strings.Contains(plain(m.View().Content), "Looking at the router.") {
		t.Fatalf("esc should return to the agents section:\n%s", plain(m.View().Content))
	}
}

func TestBackgroundSessionKeepsReceivingEvents(t *testing.T) {
	f := withEvents(seeded())
	m := ready(t, f)
	m, wait := openSession(t, m)
	m, back := key(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	next, _ := m.Update(back())
	m = next.(Model)
	f.Emit("s1", orchestration.SessionEvent{Type: "output", Text: "Found it in router.go"})
	next, wait2 := m.Update(wait())
	m = next.(Model)
	if wait2 == nil {
		t.Fatal("a routed event should re-arm the wait for that session")
	}
	m, _ = openSession(t, m)
	if !strings.Contains(plain(m.View().Content), "Found it in router.go") {
		t.Fatalf("background events must reach the session:\n%s", plain(m.View().Content))
	}
}

func TestDeletedSessionIsDropped(t *testing.T) {
	f := withEvents(seeded())
	m := ready(t, f)
	m, _ = openSession(t, m)
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyTab}) // feed focus
	m, _ = press(m, 'D')
	m, cmd := press(m, 'y')
	if cmd == nil {
		t.Fatal("confirming should run the delete")
	}
	m = driveRoot(m, cmd)
	if len(f.Sessions) != 1 {
		t.Fatalf("session should be deleted: %+v", f.Sessions)
	}
	if m.OpenSessions() != 0 || strings.Contains(plain(m.View().Content), "Looking at the router.") {
		t.Fatalf("deleted session should be dropped from the root, open=%d", m.OpenSessions())
	}
}

func TestQuitClosesSubscriptions(t *testing.T) {
	f := withEvents(seeded())
	f.Sessions = f.Sessions[:1]
	f.Sessions[0].Status = domain.SessionDone
	m := ready(t, f)
	m, _ = openSession(t, m)
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyTab})
	_, cmd := key(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("want QuitMsg got %T", cmd())
	}
}

var _ session.Routed = session.EventMsg{}
