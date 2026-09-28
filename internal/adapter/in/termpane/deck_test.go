//go:build !windows

package termpane

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// deckRun drives a Deck the way the Bubble Tea runtime would: every returned
// command runs on its own goroutine and its message is fed back to Update.
// Messages the deck does not handle itself (ClosedMsg, host commands, quit)
// are kept for the test to inspect.
type deckRun struct {
	t      *testing.T
	d      Deck
	msgs   chan tea.Msg
	toHost []tea.Msg
}

func newDeckRun(t *testing.T, cmds ...Command) *deckRun {
	t.Helper()
	r := &deckRun{t: t, d: NewDeck(cmds...), msgs: make(chan tea.Msg, 256)}
	t.Cleanup(func() { r.d.CloseAll() })
	r.send(tea.WindowSizeMsg{Width: 60, Height: 10})
	return r
}

// add starts a pane running script with /bin/sh and adds it to the deck.
func (r *deckRun) add(script string) Model {
	r.t.Helper()
	w, h := r.d.paneSize()
	p := New(Options{Command: "/bin/sh", Args: []string{"-c", script}, Width: w, Height: h})
	if err := p.Start(); err != nil {
		r.t.Fatal(err)
	}
	var cmd tea.Cmd
	r.d, cmd = r.d.Add(p)
	r.exec(cmd)
	return p
}

func (r *deckRun) exec(cmd tea.Cmd) {
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

func (r *deckRun) send(msg tea.Msg) {
	var cmd tea.Cmd
	r.d, cmd = r.d.Update(msg)
	r.exec(cmd)
}

// command presses the prefix and then key.
func (r *deckRun) command(key tea.KeyPressMsg) {
	r.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	r.send(key)
}

func (r *deckRun) until(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case msg := <-r.msgs:
			switch msg.(type) {
			case FrameMsg, ExitedMsg:
				r.send(msg)
			default:
				r.toHost = append(r.toHost, msg)
			}
		case <-deadline:
			r.t.Fatalf("timed out waiting for %s; view:\n%s", what, r.view())
		}
	}
}

func (r *deckRun) view() string { return ansi.Strip(r.d.View()) }

func (r *deckRun) tabBar() string { return strings.SplitN(r.view(), "\n", 2)[0] }

func (r *deckRun) shows(s string) func() bool {
	return func() bool { return strings.Contains(r.view(), s) }
}

func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func TestDeckSwitchesPanesAndMarksBackgroundOutput(t *testing.T) {
	r := newDeckRun(t)
	r.add(`printf 'first\r\n'; sleep 0.5; printf 'late\r\n'; cat`)
	r.until("pane 1", r.shows("first"))

	r.add(`printf 'second\r\n'; cat`)
	r.until("pane 2", r.shows("second"))
	if r.d.Active() != 1 || r.d.Len() != 2 {
		t.Fatalf("after add: active %d of %d, want 1 of 2", r.d.Active(), r.d.Len())
	}

	// Pane 1 prints while pane 2 is focused: its tab gets the unseen marker.
	r.until("unseen marker on tab 1", func() bool { return strings.Contains(r.tabBar(), " 1 sh ● ") })

	r.command(key('1'))
	if r.d.Active() != 0 {
		t.Fatalf("after 1: active %d, want 0", r.d.Active())
	}
	if !strings.Contains(r.view(), "late") {
		t.Errorf("pane 1 does not show its background output:\n%s", r.view())
	}
	if strings.Contains(r.tabBar(), "●") {
		t.Errorf("focusing pane 1 kept the unseen marker: %q", r.tabBar())
	}

	r.command(key('n'))
	if r.d.Active() != 1 {
		t.Errorf("after n: active %d, want 1", r.d.Active())
	}
	r.command(key('n'))
	if r.d.Active() != 0 {
		t.Errorf("n wraps: active %d, want 0", r.d.Active())
	}
	r.command(key('p'))
	if r.d.Active() != 1 {
		t.Errorf("p wraps: active %d, want 1", r.d.Active())
	}
}

func TestDeckKeysGoToFocusedPaneOnly(t *testing.T) {
	r := newDeckRun(t)
	r.add(`read x; printf 'one:%s\r\n' "$x"; cat`)
	r.add(`read x; printf 'two:%s\r\n' "$x"; cat`)
	for _, k := range []tea.KeyPressMsg{key('z'), {Code: tea.KeyEnter}} {
		r.send(k)
	}
	r.until("pane 2 echo", r.shows("two:z"))

	r.command(key('1'))
	if strings.Contains(r.view(), "one:") {
		t.Errorf("keys typed in pane 2 reached pane 1:\n%s", r.view())
	}
}

func TestDeckTabBarShowsTitlesAndExitStatus(t *testing.T) {
	r := newDeckRun(t)
	r.add(`printf '\033]0;fix flaky tests\007'; cat`)
	r.until("title", func() bool { return strings.Contains(r.tabBar(), " 1 fix flaky tests ") })

	r.add(`exit 0`)
	r.add(`exit 3`)
	r.until("exit marks", func() bool {
		bar := r.tabBar()
		return strings.Contains(bar, " 2 sh ✓ ") && strings.Contains(bar, " 3 sh ✗ ")
	})
	if r.d.Live() != 1 {
		t.Errorf("Live() = %d, want 1", r.d.Live())
	}
}

type openSessionMsg struct{}

func TestDeckHostCommandsAndHints(t *testing.T) {
	r := newDeckRun(t, Command{Key: "c", Label: "new session", Msg: func() tea.Msg { return openSessionMsg{} }})
	r.add(`cat`)

	if !strings.Contains(r.tabBar(), "ctrl+] for commands") {
		t.Errorf("idle hint missing: %q", r.tabBar())
	}
	r.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if bar := r.tabBar(); !strings.Contains(bar, "c new session") {
		t.Errorf("host command missing from hint: %q", bar)
	}
	if r.d.Cursor() != nil {
		t.Error("cursor shown while a command is pending")
	}
	r.send(key('c'))
	r.until("host command message", func() bool {
		for _, m := range r.toHost {
			if _, ok := m.(openSessionMsg); ok {
				return true
			}
		}
		return false
	})

	r.command(key('7'))
	if !strings.Contains(r.tabBar(), "no pane 7") {
		t.Errorf("missing pane not reported: %q", r.tabBar())
	}
	r.command(key('?'))
	if !strings.Contains(r.tabBar(), `unknown command "?"`) {
		t.Errorf("unknown command not reported: %q", r.tabBar())
	}
}

func TestDeckResizesEveryPane(t *testing.T) {
	r := newDeckRun(t)
	r.add(`read x; stty size; cat`)
	r.add(`read x; stty size; cat`)

	r.send(tea.WindowSizeMsg{Width: 50, Height: 11}) // panes get one row less
	r.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.until("pane 2 size", r.shows("10 50"))

	r.command(key('1'))
	r.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.until("background pane 1 resized too", r.shows("10 50"))
}

func TestDeckCloseReportsAndFocusesNeighbour(t *testing.T) {
	r := newDeckRun(t)
	first := r.add(`printf 'first\r\n'; cat`)
	r.until("pane 1", r.shows("first"))
	second := r.add(`printf 'second\r\n'; cat`)
	r.until("pane 2", r.shows("second"))

	closed := func(id int64) func() bool {
		return func() bool {
			for _, m := range r.toHost {
				if c, ok := m.(ClosedMsg); ok && c.ID == id {
					return true
				}
			}
			return false
		}
	}

	r.command(key('x'))
	if r.d.Len() != 1 || r.d.Active() != 0 || r.d.Owns(second.ID()) {
		t.Fatalf("after close: active %d of %d, want 0 of 1 without pane 2", r.d.Active(), r.d.Len())
	}
	r.until("ClosedMsg for pane 2", closed(second.ID()))
	if exited, _ := second.Exited(); !exited {
		t.Error("closed pane's process still running")
	}
	if !strings.Contains(r.view(), "first") {
		t.Errorf("remaining pane not shown:\n%s", r.view())
	}

	// Closing the last pane leaves an empty deck; quitting is the host's call.
	r.command(key('x'))
	r.until("ClosedMsg for pane 1", closed(first.ID()))
	if r.d.Len() != 0 {
		t.Fatalf("after last close: %d panes", r.d.Len())
	}
	for _, m := range r.toHost {
		if _, ok := m.(tea.QuitMsg); ok {
			t.Error("closing the last pane quit")
		}
	}
}

func TestDeckFocusByID(t *testing.T) {
	r := newDeckRun(t)
	first := r.add(`cat`)
	r.add(`cat`)
	r.d = r.d.Focus(first.ID())
	if r.d.Active() != 0 {
		t.Fatalf("Focus: active %d, want 0", r.d.Active())
	}
}
