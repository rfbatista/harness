package tui

import (
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

// TestProgramRunsHeadless boots the real program with no renderer, lets it
// load a snapshot, and quits it. It catches wiring that only breaks at Run.
func TestProgramRunsHeadless(t *testing.T) {
	m := New(Options{Backend: seeded(), Theme: theme.Dark(), RefreshInterval: 10 * time.Millisecond})
	p := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil), tea.WithOutput(io.Discard))
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
	time.Sleep(30 * time.Millisecond)
	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(3 * time.Second):
		p.Kill()
		t.Fatal("program did not quit")
	}
}
