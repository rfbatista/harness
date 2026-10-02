// Package term shows running terminals inside a Bubble Tea program: a pane
// (Model) per terminal, and a tabbed Deck of them.
//
// A pane does not run anything. It attaches to a ports.Terminal — hosted in
// this process or on the server — and draws a copy of its screen: the
// snapshot it attached with, then every byte printed after, parsed by a local
// VT emulator. Keys go the other way as events, and the host encodes them for
// the modes the program has set.
package term

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"

	"operators-mcp/internal/ports"
)

var nextID atomic.Int64

// Options is the terminal a pane shows and the pane's initial size.
type Options struct {
	Terminal ports.Terminal
	// Name labels the pane until the program sets a title.
	Name   string
	Width  int
	Height int
}

// FrameMsg says the pane's screen changed and should be redrawn.
type FrameMsg struct{ ID int64 }

// ExitedMsg says the pane's process ended with Code; Err is set when the
// code is not zero.
type ExitedMsg struct {
	ID   int64
	Code int
	Err  error
}

// Model is a Bubble Tea component showing one terminal. Copies share the same
// attachment, so it is safe to pass by value like any tea model.
type Model struct {
	id   int64
	opts Options
	a    *attachment
}

// attachment is a pane's local copy of a terminal's screen.
type attachment struct {
	t        ports.Terminal
	emu      *vt.SafeEmulator
	sub      atomic.Pointer[ports.Subscription]
	detached atomic.Bool
}

// New builds a pane. Start attaches it.
func New(opts Options) Model {
	if opts.Width <= 0 {
		opts.Width = 80
	}
	if opts.Height <= 0 {
		opts.Height = 24
	}
	return Model{id: nextID.Add(1), opts: opts}
}

// ID identifies this pane's messages when several panes coexist.
func (m Model) ID() int64 { return m.id }

// Start attaches to the terminal: its screen as it is now, then everything
// it prints.
func (m *Model) Start() error {
	if m.a != nil {
		return fmt.Errorf("term: already started")
	}
	if m.opts.Terminal == nil {
		return fmt.Errorf("term: no terminal")
	}
	a := &attachment{t: m.opts.Terminal, emu: vt.NewSafeEmulator(m.opts.Width, m.opts.Height)}
	// The local emulator answers terminal queries too; the host already has,
	// so its answers are drained and dropped.
	go func() { _, _ = io.Copy(io.Discard, a.emu) }()
	a.subscribe()
	m.a = a
	return nil
}

// subscribe (re)draws the local screen from a fresh snapshot and follows the
// output after it.
func (a *attachment) subscribe() {
	snap, sub := a.t.Subscribe()
	if snap.Size.Cols > 0 && snap.Size.Rows > 0 &&
		(snap.Size.Cols != a.emu.Width() || snap.Size.Rows != a.emu.Height()) {
		a.emu.Resize(snap.Size.Cols, snap.Size.Rows)
	}
	var b strings.Builder
	if snap.AltScreen {
		b.WriteString("\x1b[?1049h")
	}
	b.WriteString("\x1b[0m\x1b[2J\x1b[H")
	b.WriteString(strings.ReplaceAll(snap.Screen, "\n", "\r\n"))
	fmt.Fprintf(&b, "\x1b[0m\x1b[%d;%dH", snap.CursorY+1, snap.CursorX+1)
	_, _ = a.emu.Write([]byte(b.String()))
	if old := a.sub.Swap(&sub); old != nil {
		old.Close()
	}
}

// Init starts listening for screen updates.
func (m Model) Init() tea.Cmd { return m.wait() }

// wait delivers the next output to the local screen. A subscription that
// closes while the terminal still runs means the pane fell behind: it
// attaches again for a fresh snapshot.
func (m Model) wait() tea.Cmd {
	a, id := m.a, m.id
	if a == nil {
		return nil
	}
	return func() tea.Msg {
		for {
			sub := a.sub.Load()
			b, ok := <-sub.C
			if ok {
				_, _ = a.emu.Write(b)
				drain(a, sub.C)
				return FrameMsg{ID: id}
			}
			if a.detached.Load() {
				return nil
			}
			select {
			case <-a.t.Done():
				a.closeInput()
				code := a.t.ExitCode()
				var err error
				if code != 0 {
					err = fmt.Errorf("exit status %d", code)
				}
				return ExitedMsg{ID: id, Code: code, Err: err}
			default:
				a.subscribe()
			}
		}
	}
}

// drain writes whatever output is already queued, so a burst is one redraw.
func drain(a *attachment, c <-chan []byte) {
	for {
		select {
		case b, ok := <-c:
			if !ok {
				return
			}
			_, _ = a.emu.Write(b)
		default:
			return
		}
	}
}

// Update handles the pane's own messages, key presses, pastes and resizes.
// The host decides which keys reach the pane; everything it forwards goes to
// the terminal.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.a == nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case FrameMsg:
		if msg.ID == m.id {
			return m, m.wait()
		}
	case ExitedMsg:
		// Nothing to re-arm: the process is gone.
	case tea.KeyPressMsg:
		if !m.exited() {
			_ = m.a.t.Key(keyEvent(msg))
		}
	case tea.PasteMsg:
		if !m.exited() {
			_ = m.a.t.Paste(msg.Content)
		}
	case tea.WindowSizeMsg:
		m.Resize(msg.Width, msg.Height)
	}
	return m, nil
}

func keyEvent(k tea.KeyPressMsg) ports.KeyEvent {
	return ports.KeyEvent{
		Code:        k.Code,
		Text:        k.Text,
		Mod:         int(k.Mod),
		ShiftedCode: k.ShiftedCode,
		BaseCode:    k.BaseCode,
		IsRepeat:    k.IsRepeat,
	}
}

// Resize sizes the local screen and the terminal; the program gets SIGWINCH
// and redraws.
func (m *Model) Resize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	m.opts.Width, m.opts.Height = width, height
	if m.a == nil {
		return
	}
	m.a.emu.Resize(width, height)
	_ = m.a.t.Resize(ports.TermSize{Cols: width, Rows: height})
}

// View renders the terminal's screen as styled text.
func (m Model) View() string {
	if m.a == nil {
		return ""
	}
	return m.a.emu.Render()
}

// Cursor is the program's cursor, relative to the pane's top-left corner, or
// nil once it has exited. Hosts offset it by where they place the pane.
func (m Model) Cursor() *tea.Cursor {
	if m.a == nil || m.exited() {
		return nil
	}
	p := m.a.emu.CursorPosition()
	return tea.NewCursor(p.X, p.Y)
}

// Title is the window title the program last set (OSC 0/2), else
// Options.Name. claude uses it for the current task, with a spinner while it
// works.
func (m Model) Title() string {
	if m.a != nil {
		if t := m.a.t.Title(); t != "" {
			return t
		}
	}
	return m.opts.Name
}

func (m Model) exited() bool {
	select {
	case <-m.a.t.Done():
		return true
	default:
		return false
	}
}

// Exited reports whether the process has ended, and with what error.
func (m Model) Exited() (bool, error) {
	if m.a == nil || !m.exited() {
		return false, nil
	}
	if code := m.a.t.ExitCode(); code != 0 {
		return true, fmt.Errorf("exit status %d", code)
	}
	return true, nil
}

// Close stops the terminal's process and detaches. Safe to call more than
// once.
func (m Model) Close() error {
	if m.a == nil {
		return nil
	}
	err := m.a.t.Kill()
	if sub := m.a.sub.Load(); sub != nil {
		sub.Close()
	}
	m.a.closeInput()
	return err
}

// Detach stops showing the terminal and leaves its process running: for a
// session the server runs, which outlives this client.
func (m Model) Detach() {
	if m.a == nil {
		return
	}
	m.a.detached.Store(true)
	if sub := m.a.sub.Load(); sub != nil {
		sub.Close()
	}
	m.a.closeInput()
}

// closeInput ends the local emulator's input pipe, and with it the goroutine
// draining its answers.
func (a *attachment) closeInput() {
	if c, ok := a.emu.InputPipe().(io.Closer); ok {
		_ = c.Close()
	}
}
