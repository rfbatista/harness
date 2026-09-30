// Package term embeds an interactive terminal program, such as the claude
// CLI, inside a Bubble Tea program.
//
// The child runs on a pseudo-terminal, so it behaves exactly as it would in a
// real terminal. Its output is parsed by an in-memory VT emulator
// (charmbracelet/x/vt), and View renders the emulator's screen grid. Keys the
// host forwards to Update are encoded by the emulator and written back into
// the pseudo-terminal.
package term

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// ErrNotStarted is returned by operations that need a running child.
var ErrNotStarted = errors.New("term: not started")

var nextID atomic.Int64

// Options describes the child process and the pane's initial size.
type Options struct {
	Command string
	Args    []string
	Env     []string // appended to os.Environ(); TERM defaults to xterm-256color
	Dir     string
	// Name labels the pane until the child sets a title; empty uses the
	// command's name.
	Name   string
	Width  int
	Height int
	// KillAfter bounds how long Close waits after SIGTERM before SIGKILL.
	KillAfter time.Duration
}

// FrameMsg says the pane's screen changed and should be redrawn.
type FrameMsg struct{ ID int64 }

// ExitedMsg says the child process ended. Err is nil on a clean exit.
type ExitedMsg struct {
	ID  int64
	Err error
}

// Model is a Bubble Tea component showing one child process. Copies share the
// same underlying session, so it is safe to pass by value like any tea model.
type Model struct {
	id   int64
	opts Options
	s    *session
}

// New builds a pane. Start launches the child.
func New(opts Options) Model {
	if opts.Width <= 0 {
		opts.Width = 80
	}
	if opts.Height <= 0 {
		opts.Height = 24
	}
	if opts.KillAfter <= 0 {
		opts.KillAfter = 2 * time.Second
	}
	return Model{id: nextID.Add(1), opts: opts}
}

// ID identifies this pane's messages when several panes coexist.
func (m Model) ID() int64 { return m.id }

// Start launches the child on a pseudo-terminal sized to the pane.
func (m *Model) Start() error {
	if m.s != nil {
		return fmt.Errorf("term: already started")
	}
	cmd := exec.Command(m.opts.Command, m.opts.Args...)
	cmd.Dir = m.opts.Dir
	cmd.Env = append(append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor"), m.opts.Env...)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(m.opts.Width),
		Rows: uint16(m.opts.Height),
	})
	if err != nil {
		return fmt.Errorf("term: start %s: %w", m.opts.Command, err)
	}
	emu := vt.NewSafeEmulator(m.opts.Width, m.opts.Height)
	m.s = newSession(cmd, ptmx, emu)
	emu.SetCallbacks(vt.Callbacks{Title: m.s.setTitle}) // before run: not locked
	m.s.run()
	return nil
}

// Init starts listening for screen updates.
func (m Model) Init() tea.Cmd { return m.wait() }

// Update handles the pane's own messages, key presses, pastes and resizes.
// The host decides which keys reach the pane; everything it forwards is sent
// to the child.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.s == nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case FrameMsg:
		if msg.ID == m.id {
			return m, m.wait()
		}
	case ExitedMsg:
		// Nothing to re-arm: the child is gone.
	case tea.KeyPressMsg:
		if !m.s.exited() {
			sendKey(m.s.emu, msg)
		}
	case tea.PasteMsg:
		if !m.s.exited() {
			m.s.emu.Paste(msg.Content)
		}
	case tea.WindowSizeMsg:
		m.Resize(msg.Width, msg.Height)
	}
	return m, nil
}

// Resize changes the emulator and the pseudo-terminal size; the child gets
// SIGWINCH and redraws.
func (m *Model) Resize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	m.opts.Width, m.opts.Height = width, height
	if m.s == nil {
		return
	}
	m.s.emu.Resize(width, height)
	_ = pty.Setsize(m.s.ptmx, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
}

// View renders the child's screen as styled text.
func (m Model) View() string {
	if m.s == nil {
		return ""
	}
	return m.s.emu.Render()
}

// Cursor is the child's cursor, relative to the pane's top-left corner, or
// nil when the child has exited. Hosts offset it by where they place the pane.
func (m Model) Cursor() *tea.Cursor {
	if m.s == nil || m.s.exited() {
		return nil
	}
	p := m.s.emu.CursorPosition()
	return tea.NewCursor(p.X, p.Y)
}

// Title is the window title the child last set (OSC 0/2), else Options.Name,
// else the command's name. claude uses it for the current task, with a spinner
// while it works.
func (m Model) Title() string {
	if m.s != nil {
		if t := m.s.title.Load(); t != nil && *t != "" {
			return *t
		}
	}
	if m.opts.Name != "" {
		return m.opts.Name
	}
	return filepath.Base(m.opts.Command)
}

// Exited reports whether the child has ended, and with what error.
func (m Model) Exited() (bool, error) {
	if m.s == nil {
		return false, nil
	}
	if !m.s.exited() {
		return false, nil
	}
	return true, m.s.waitErr
}

// Close stops the child: SIGTERM, then SIGKILL after Options.KillAfter. It
// waits for the pump goroutines to finish. Safe to call more than once.
func (m Model) Close() error {
	if m.s == nil {
		return ErrNotStarted
	}
	return m.s.close(m.opts.KillAfter)
}

func (m Model) wait() tea.Cmd {
	s, id := m.s, m.id
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-s.frames:
			return FrameMsg{ID: id}
		case <-s.done:
			return ExitedMsg{ID: id, Err: s.waitErr}
		}
	}
}

func terminate(p *os.Process, after time.Duration, done <-chan struct{}) {
	_ = p.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(after):
		_ = p.Kill()
	}
}
