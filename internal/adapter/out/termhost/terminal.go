package termhost

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"operators-mcp/internal/ports"
)

// defaultSubscriberBuffer is how many output chunks a subscriber may fall
// behind before it is dropped and must subscribe again for a fresh snapshot.
const defaultSubscriberBuffer = 1024

// scrollbackLines is how many lines of main-screen history a terminal keeps
// and hands to a client in its snapshot: the attach protocol's bound.
const scrollbackLines = 2000

// terminal is one process on a PTY and the goroutines that connect it to the
// emulator:
//
//	process ──pty──▶ output loop ──Write──▶ emulator, and every subscriber
//	process ◀──pty── replies loop ◀──Read── emulator ◀──SendKey── Key
//
// The replies loop matters beyond keys: the emulator answers terminal queries
// (cursor position, device attributes) through the same pipe, and programs
// such as claude block until those answers arrive — whether or not anyone is
// watching.
type terminal struct {
	proc      ports.PTYProcess
	emu       *vt.SafeEmulator
	killAfter time.Duration
	subBuffer int

	// mu makes feeding the emulator and fanning out to subscribers one step,
	// so a snapshot taken under it is followed by exactly the output after it.
	mu   sync.Mutex
	size ports.TermSize
	subs map[chan []byte]struct{}

	title    atomic.Pointer[string]
	done     chan struct{}
	code     int
	killOnce sync.Once
	killErr  error
	replies  sync.WaitGroup
}

func newTerminal(proc ports.PTYProcess, size ports.TermSize, killAfter time.Duration, subBuffer int) *terminal {
	t := &terminal{
		proc:      proc,
		emu:       vt.NewSafeEmulator(max(1, size.Cols), max(1, size.Rows)),
		killAfter: killAfter,
		subBuffer: subBuffer,
		size:      size,
		subs:      map[chan []byte]struct{}{},
		done:      make(chan struct{}),
	}
	t.emu.SetCallbacks(vt.Callbacks{Title: func(s string) { t.title.Store(&s) }}) // before run: not locked
	t.emu.SetScrollbackSize(scrollbackLines)
	return t
}

func (t *terminal) run(onExit func(code int)) {
	t.replies.Add(1)
	go func() {
		defer t.replies.Done()
		_, _ = io.Copy(t.proc, t.emu)
		// If the pty stopped accepting writes first, close the input so a
		// pending key does not hold the emulator lock forever.
		t.closeInput()
	}()
	go t.output(onExit)
}

// output feeds everything the process prints to the emulator and the
// subscribers. It ends when the pty reports the process side is gone, then
// reaps the process, so Done means "no more output".
func (t *terminal) output(onExit func(code int)) {
	buf := make([]byte, 32*1024)
	for {
		n, err := t.proc.Read(buf)
		if n > 0 {
			t.feed(buf[:n])
		}
		if err != nil {
			break
		}
	}
	code, _ := t.proc.Wait()
	// Unblocks the replies loop and makes later keys fail fast instead of
	// blocking on a pipe nobody reads.
	t.closeInput()
	t.replies.Wait()
	_ = t.proc.Close()

	t.mu.Lock()
	t.code = code
	close(t.done) // before the subscriptions, so a closed one reads as exited
	for c := range t.subs {
		close(c)
	}
	t.subs = nil
	t.mu.Unlock()

	if onExit != nil {
		onExit(code)
	}
}

func (t *terminal) feed(b []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = t.emu.Write(b)
	for c := range t.subs {
		select {
		case c <- append([]byte(nil), b...):
		default:
			// Fell too far behind: drop it rather than stall the process.
			// It sees its channel close while the terminal lives on, and
			// subscribes again.
			close(c)
			delete(t.subs, c)
		}
	}
}

// closeInput ends the emulator's input pipe. Emulator.Close would do the same
// but also flips an unsynchronised flag that Read checks, which races with the
// replies loop; the pipe itself is safe for concurrent use.
func (t *terminal) closeInput() {
	if c, ok := t.emu.InputPipe().(io.Closer); ok {
		_ = c.Close()
	}
}

func (t *terminal) exited() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

// Subscribe takes the screen, its scrollback and the subscription in one
// step under mu, so the output that follows is exactly what was printed
// after them. The scrollback is rendered after unlocking: its lines are
// immutable once stored, so copying the slice is enough.
func (t *terminal) Subscribe() (ports.TerminalSnapshot, ports.Subscription) {
	t.mu.Lock()
	pos := t.emu.CursorPosition()
	snap := ports.TerminalSnapshot{
		Screen:    t.emu.Render(),
		CursorX:   pos.X,
		CursorY:   pos.Y,
		AltScreen: t.emu.IsAltScreen(),
		Size:      t.size,
		Title:     t.Title(),
	}
	var history []uv.Line
	if !snap.AltScreen {
		history = slices.Clone(t.emu.Scrollback().Lines())
	}
	c := make(chan []byte, t.subBuffer)
	exited := t.subs == nil
	if !exited {
		t.subs[c] = struct{}{}
	}
	t.mu.Unlock()

	snap.Scrollback = renderScrollback(history)
	if exited {
		close(c)
		return snap, ports.Subscription{C: c, Close: func() {}}
	}
	return snap, ports.Subscription{C: c, Close: func() { t.unsubscribe(c) }}
}

// renderScrollback renders history the way the emulator renders the screen:
// each line styled, lines joined by "\n", oldest first.
func renderScrollback(history []uv.Line) string {
	if len(history) == 0 {
		return ""
	}
	var b strings.Builder
	for i, l := range history {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.Render())
	}
	return b.String()
}

func (t *terminal) unsubscribe(c chan []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.subs[c]; ok {
		delete(t.subs, c)
		close(c)
	}
}

// Key forwards one key press to the process.
//
// Printable input goes as text: the emulator's key encoder only emits the bare
// key code for unmodified keys, so shift+a would otherwise be dropped. Keys
// with ctrl or alt, and special keys, go through the encoder so they honour
// the program's cursor-key and keypad modes.
func (t *terminal) Key(k ports.KeyEvent) error {
	if t.exited() {
		return nil
	}
	mod := uv.KeyMod(k.Mod)
	if k.Text != "" && mod&(uv.ModCtrl|uv.ModAlt|uv.ModMeta) == 0 {
		t.emu.SendText(k.Text)
		return nil
	}
	t.emu.SendKey(uv.KeyPressEvent{
		Text:        k.Text,
		Mod:         mod,
		Code:        k.Code,
		ShiftedCode: k.ShiftedCode,
		BaseCode:    k.BaseCode,
		IsRepeat:    k.IsRepeat,
	})
	return nil
}

func (t *terminal) Paste(text string) error {
	if !t.exited() {
		t.emu.Paste(text)
	}
	return nil
}

// Resize changes the emulator and the terminal size; the process gets
// SIGWINCH and redraws.
func (t *terminal) Resize(size ports.TermSize) error {
	if size.Cols <= 0 || size.Rows <= 0 {
		return nil
	}
	t.mu.Lock()
	t.size = size
	t.emu.Resize(size.Cols, size.Rows)
	t.mu.Unlock()
	if t.exited() {
		return nil
	}
	return t.proc.Resize(size)
}

func (t *terminal) Title() string {
	if p := t.title.Load(); p != nil {
		return *p
	}
	return ""
}

// Kill sends SIGTERM, then SIGKILL after the grace period, and waits for the
// process and its output to finish. Safe to call more than once.
func (t *terminal) Kill() error {
	t.killOnce.Do(func() {
		if t.exited() {
			return
		}
		if err := t.proc.Signal(syscall.SIGTERM); err != nil {
			if !errors.Is(err, os.ErrProcessDone) {
				t.killErr = err
				return
			}
		}
		select {
		case <-t.done:
		case <-time.After(t.killAfter):
			_ = t.proc.Signal(syscall.SIGKILL)
			<-t.done
		}
	})
	return t.killErr
}

func (t *terminal) Done() <-chan struct{} { return t.done }

func (t *terminal) ExitCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.code
}
