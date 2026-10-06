package ports

import "context"

// TerminalHost runs interactive agents on terminals it owns and lets
// clients attach to them. It composes Agent, Shell and PTY; where it runs —
// in the server, or inside tui-client — is a wiring choice.
type TerminalHost interface {
	// Spawn starts spec on a terminal of the given size under id. onExit, if
	// set, is called once with the exit code after the process is gone and
	// all its output has been delivered.
	Spawn(ctx context.Context, id string, spec AgentSpec, size TermSize, onExit func(code int)) error
	// Attach returns the terminal running under id, or TERMINAL_NOT_FOUND.
	Attach(id string) (Terminal, error)
}

// Terminal is one running agent's terminal, as a client sees it. The host
// keeps the authoritative screen; a client draws a copy of it from
// Subscribe's snapshot and the output that follows.
type Terminal interface {
	// Subscribe returns the screen as it is now and a stream of every byte
	// printed after it, taken together so nothing falls between them. The
	// channel is closed when the process is gone — Done is closed first — or
	// when the subscriber fell too far behind, in which case it subscribes
	// again for a fresh snapshot.
	Subscribe() (TerminalSnapshot, Subscription)
	// Key sends one key press; the host encodes it for the modes the program
	// has set (application cursor keys and the like).
	Key(KeyEvent) error
	// Paste sends text as a bracketed paste when the program asked for one.
	Paste(text string) error
	Resize(TermSize) error
	// Title is the window title the program last set.
	Title() string
	// Kill stops the process — SIGTERM, then SIGKILL after a grace period —
	// and returns once it is gone.
	Kill() error
	// Done is closed when the process has exited.
	Done() <-chan struct{}
	// ExitCode is the process's exit code, once Done is closed.
	ExitCode() int
}

// TerminalSnapshot is a terminal's screen at one moment.
type TerminalSnapshot struct {
	// Screen is the visible screen, rendered with its styles, one line per
	// row.
	Screen string `json:"screen"`
	// Scrollback is what scrolled off the top of the main screen, rendered
	// like Screen, oldest line first, the last line being the one just
	// above the first visible row. At most 2000 lines; absent on the
	// alternate screen, which has no history, and on Log snapshots, whose
	// Screen already is the whole output. Lines keep the width they had
	// when they scrolled off.
	Scrollback string   `json:"scrollback,omitempty"`
	CursorX    int      `json:"cursor_x"`
	CursorY    int      `json:"cursor_y"`
	AltScreen  bool     `json:"alt_screen,omitempty"`
	Size       TermSize `json:"size"`
	Title      string   `json:"title,omitempty"`
	// Log marks Screen as raw output to write as it is (an application
	// run's log so far), not a rendered screen: no cursor to place.
	Log bool `json:"log,omitempty"`
}

// Subscription is a stream of terminal output.
type Subscription struct {
	C     <-chan []byte
	Close func()
}

// KeyEvent is one key press, as tui-client's terminal library reports it.
type KeyEvent struct {
	Code        rune   `json:"code"`
	Text        string `json:"text,omitempty"`
	Mod         int    `json:"mod,omitempty"`
	ShiftedCode rune   `json:"shifted_code,omitempty"`
	BaseCode    rune   `json:"base_code,omitempty"`
	IsRepeat    bool   `json:"is_repeat,omitempty"`
}

// TerminalMessage is one text frame of the terminal attach protocol, spoken
// over a WebSocket at /api/sessions/:id/terminal. Output travels as binary
// frames, in order, between them.
//
// Server to client: "snapshot" first — the screen with its scrollback — and
// again whenever the client fell behind and the screen is redrawn from
// scratch; "title" when the program sets one; "exit" with the code once the
// process is gone. Client to server: "key", "paste", "resize", and "resync"
// when the client fell behind and needs a fresh snapshot.
type TerminalMessage struct {
	Type     string            `json:"type"`
	Snapshot *TerminalSnapshot `json:"snapshot,omitempty"`
	Title    string            `json:"title,omitempty"`
	Code     int               `json:"code,omitempty"`
	Key      *KeyEvent         `json:"key,omitempty"`
	Text     string            `json:"text,omitempty"`
	Size     *TermSize         `json:"size,omitempty"`
}
