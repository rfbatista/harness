package web

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/coder/websocket"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A full-screen program in the Agent tab, as claude runs today: it draws on
// the alternate screen, which has no history to scroll, and turns on mouse
// reporting to scroll its own view. The socket carries keys, so the wheel
// reaches it as PageUp / PageDown — and only when it asked for the mouse.

// fullscreenPTY answers an attach with an alternate-screen snapshot, then
// prints mouse sequences (claude's ?1000h ?1002h ?1003h ?1006h when mouse is
// set), and records what the page sends.
type fullscreenPTY struct {
	mouse    bool
	mu       sync.Mutex
	received []ports.TerminalMessage
}

func (p *fullscreenPTY) keys() []rune {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []rune
	for _, m := range p.received {
		if m.Type == "key" && m.Key != nil {
			out = append(out, m.Key.Code)
		}
	}
	return out
}

func (p *fullscreenPTY) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()
	screen := make([]string, 24)
	screen[0] = "full screen top"
	screen[23] = "❯ "
	snap, _ := json.Marshal(ports.TerminalMessage{Type: "snapshot", Snapshot: &ports.TerminalSnapshot{
		Screen: strings.Join(screen, "\n"), CursorX: 2, CursorY: 23, AltScreen: true, Size: ports.TermSize{Cols: 80, Rows: 24}, Title: "claude",
	}})
	_ = conn.Write(ctx, websocket.MessageText, snap)
	if p.mouse {
		_ = conn.Write(ctx, websocket.MessageBinary, []byte("\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h"))
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m ports.TerminalMessage
		if typ == websocket.MessageText && json.Unmarshal(data, &m) == nil {
			p.mu.Lock()
			p.received = append(p.received, m)
			p.mu.Unlock()
		}
	}
}

const (
	codePageUp   = 1114122
	codePageDown = 1114123
)

// wheelNotches turns the wheel n notches of deltaY each over the terminal.
func wheelNotches(n int, deltaY float64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for i := 0; i < n; i++ {
			if err := wheelOverTerminal(deltaY).Do(ctx); err != nil {
				return err
			}
			if err := chromedp.Sleep(40 * time.Millisecond).Do(ctx); err != nil {
				return err
			}
		}
		return chromedp.Sleep(200 * time.Millisecond).Do(ctx)
	})
}

func openFullscreen(t *testing.T, pty *fullscreenPTY) (context.Context, *errorLog) {
	t.Helper()
	srv := sessionsPageWith(t, []*domain.Session{liveSession()}, pty)
	ctx, errs := browser(t)
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(termRows+`.includes('full screen top')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		// The list has the focus: scrolling must leave it there.
		chromedp.Focus(`[role=listbox][aria-label="Sessions of this task"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	return ctx, errs
}

// The wheel over a full-screen program that asked for the mouse: PageUp
// scrolling up, PageDown scrolling down, nothing else sent, the page and the
// focus where they were.
func TestWheelPagesAFullScreenProgramThatAskedForTheMouse(t *testing.T) {
	pty := &fullscreenPTY{mouse: true}
	ctx, errs := openFullscreen(t, pty)
	var focusBefore, focusAfter string
	var pageScroll float64
	err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.activeElement.getAttribute('aria-label')`, &focusBefore),
		wheelNotches(8, -120),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	up := pty.keys()
	err = chromedp.Run(ctx,
		wheelNotches(8, 120),
		chromedp.Evaluate(`document.activeElement.getAttribute('aria-label')`, &focusAfter),
		chromedp.Evaluate(`document.scrollingElement.scrollTop`, &pageScroll),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	all := pty.keys()
	down := all[len(up):]
	if len(up) == 0 || slices.ContainsFunc(up, func(c rune) bool { return c != codePageUp }) {
		t.Errorf("wheel up sent keys %v, want PageUp (%d) only", up, codePageUp)
	}
	if len(down) == 0 || slices.ContainsFunc(down, func(c rune) bool { return c != codePageDown }) {
		t.Errorf("wheel down sent keys %v, want PageDown (%d) only", down, codePageDown)
	}
	// Eight notches of 120px over a pane of ~600px: a few pages, never one per notch.
	if len(up) >= 8 {
		t.Errorf("eight notches sent %d keys; a page key is half a pane of scrolling", len(up))
	}
	if focusBefore != focusAfter {
		t.Errorf("scrolling moved the focus from %q to %q", focusBefore, focusAfter)
	}
	if pageScroll != 0 {
		t.Errorf("the page scrolled by %v (L1, L5)", pageScroll)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A program on the alternate screen that did not ask for the mouse gets no
// keys it did not type.
func TestWheelSendsNothingToAFullScreenProgramWithoutTheMouse(t *testing.T) {
	pty := &fullscreenPTY{mouse: false}
	ctx, errs := openFullscreen(t, pty)
	if err := chromedp.Run(ctx, wheelNotches(8, -120), wheelNotches(8, 120)); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if keys := pty.keys(); len(keys) != 0 {
		t.Errorf("the wheel sent keys %v to a program that did not ask for the mouse", keys)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// H4: the screen keeps a 10,000-line snapshot history; its first line can be
// scrolled to.
func TestTerminalKeepsTenThousandLinesOfHistory(t *testing.T) {
	ctx, errs := browser(t)
	pty := &bigHistoryPTY{lines: 10_000}
	srv := sessionsPageWith(t, []*domain.Session{liveSession()}, pty)
	var first string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(termRows+`.includes('live top')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery),
		pressWithShift("Home", 36),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Poll(termFirstRow+` === 'history 1'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(termFirstRow, &first),
	)
	if err != nil {
		t.Fatalf("%v (first row %q)\nJS errors: %v", err, first, errs.all())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// bigHistoryPTY sends a snapshot with lines of main-screen history.
type bigHistoryPTY struct{ lines int }

func (p *bigHistoryPTY) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()
	history := make([]string, p.lines)
	for i := range history {
		history[i] = "history " + strconv.Itoa(i+1)
	}
	screen := make([]string, 24)
	screen[0] = "live top"
	snap, _ := json.Marshal(ports.TerminalMessage{Type: "snapshot", Snapshot: &ports.TerminalSnapshot{
		Screen: strings.Join(screen, "\n"), Scrollback: strings.Join(history, "\n"), CursorX: 0, CursorY: 23, Size: ports.TermSize{Cols: 80, Rows: 24}, Title: "claude",
	}})
	_ = conn.Write(ctx, websocket.MessageText, snap)
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}
