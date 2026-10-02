//go:build !windows

package term

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func start(t *testing.T, script string, w, h int) Model {
	t.Helper()
	m := New(Options{Terminal: shellTerminal(t, script, w, h), Name: "sh", Width: w, Height: h})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// pump runs the pane's wait command until the screen contains want or the
// child exits, the way a Bubble Tea runtime would.
func pump(t *testing.T, m Model, want string) (Model, string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	cmd := m.Init()
	for {
		screen := ansi.Strip(m.View())
		if want != "" && strings.Contains(screen, want) {
			return m, screen
		}
		msgs := make(chan tea.Msg, 1)
		go func() { msgs <- cmd() }()
		select {
		case msg := <-msgs:
			if _, ok := msg.(ExitedMsg); ok {
				screen = ansi.Strip(m.View())
				if want != "" && !strings.Contains(screen, want) {
					t.Fatalf("child exited without printing %q; screen:\n%s", want, screen)
				}
				return m, screen
			}
			m, cmd = m.Update(msg)
		case <-deadline:
			t.Fatalf("timed out waiting for %q; screen:\n%s", want, ansi.Strip(m.View()))
		}
	}
}

func TestPaneRendersOutputAndForwardsKeys(t *testing.T) {
	m := start(t, `printf 'hello \033[1;31mred\033[0m\r\n'; read x; printf 'got:%s\r\n' "$x"`, 40, 10)

	m, _ = pump(t, m, "hello red")
	for _, k := range []tea.KeyPressMsg{
		{Code: 'A', Text: "A", Mod: tea.ModShift},
		{Code: 'b', Text: "b"},
		{Code: tea.KeyEnter},
	} {
		m, _ = m.Update(k)
	}
	pump(t, m, "got:Ab")
	_, screen := pump(t, m, "") // until the child exits

	if !strings.Contains(m.View(), "\x1b[") {
		t.Errorf("View lost the child's styling: %q", m.View())
	}
	if !strings.Contains(screen, "hello red") {
		t.Errorf("earlier output scrolled away: %q", screen)
	}
	if exited, err := m.Exited(); !exited || err != nil {
		t.Errorf("Exited() = %v, %v; want true, nil", exited, err)
	}
}

func TestPaneResizeReachesChild(t *testing.T) {
	m := start(t, `read x; stty size`, 40, 10)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 77, Height: 13})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pump(t, m, "13 77")
}

func TestPaneAnswersTerminalQueries(t *testing.T) {
	// Ask for the cursor position (DSR 6) and print the raw reply. Without the
	// emulator-to-pty replies loop, dd would block forever.
	m := start(t, `stty raw -echo; printf '\033[3;5H\033[6n'; r=$(dd bs=1 count=6 2>/dev/null | od -An -c | tr -d ' \n'); stty sane; printf '\r\nreply:%s\r\n' "$r"`, 40, 10)

	pump(t, m, `reply:033[3;5R`)
}

func TestPaneCloseStopsChild(t *testing.T) {
	m := start(t, `trap '' HUP; sleep 30`, 20, 5)

	done := make(chan error, 1)
	go func() { done <- m.Close() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not stop the child")
	}
	if exited, _ := m.Exited(); !exited {
		t.Error("Exited() = false after Close")
	}
	// Keys after exit must not block on the closed emulator.
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
}
