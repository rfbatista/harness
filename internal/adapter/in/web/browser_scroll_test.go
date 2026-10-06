package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/coder/websocket"

	"operators-mcp/internal/ports"
)

// historyPTY speaks the attach protocol with a snapshot that carries
// scrollback, as the contract "terminal attach snapshot carries scrollback"
// writes it: 60 lines of history above a screen whose first row reads
// "live top" and whose last row is the prompt. The snapshot is raw JSON, so
// this test does not depend on the server's Go struct having the field. A
// resize to another height is answered with a snapshot at that height, as a
// program redrawing after SIGWINCH would; print writes output on demand;
// typed text is echoed back.
type historyPTY struct {
	mu       sync.Mutex
	received []ports.TerminalMessage
	print    chan string
}

func newHistoryPTY() *historyPTY { return &historyPTY{print: make(chan string, 8)} }

func (p *historyPTY) messages() []ports.TerminalMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ports.TerminalMessage(nil), p.received...)
}

func historySnapshot(cols, rows int) []byte {
	history := make([]string, 60)
	for i := range history {
		history[i] = fmt.Sprintf("old line %d", i+1)
	}
	screen := make([]string, rows)
	screen[0] = "live top"
	screen[rows-1] = "$ "
	snapshot, _ := json.Marshal(map[string]any{
		"type": "snapshot",
		"snapshot": map[string]any{
			"screen":     strings.Join(screen, "\n"),
			"scrollback": strings.Join(history, "\n"),
			"cursor_x":   2, "cursor_y": rows - 1,
			"size":  map[string]int{"cols": cols, "rows": rows},
			"title": "claude",
		},
	})
	return snapshot
}

func (p *historyPTY) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()

	cols, rows := 80, 24
	_ = conn.Write(ctx, websocket.MessageText, historySnapshot(cols, rows))

	go func() {
		for {
			select {
			case text := <-p.print:
				_ = conn.Write(ctx, websocket.MessageBinary, []byte(text))
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var m ports.TerminalMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		p.mu.Lock()
		p.received = append(p.received, m)
		p.mu.Unlock()
		switch {
		case m.Type == "key" && m.Key != nil && m.Key.Text != "":
			_ = conn.Write(ctx, websocket.MessageBinary, []byte(m.Key.Text))
		case m.Type == "resize" && m.Size != nil && (m.Size.Cols != cols || m.Size.Rows != rows):
			cols, rows = m.Size.Cols, m.Size.Rows
			_ = conn.Write(ctx, websocket.MessageText, historySnapshot(cols, rows))
		}
	}
}

const (
	termRows      = `(document.querySelector('.terminal .xterm-rows')?.textContent ?? '')`
	termFirstRow  = `(document.querySelector('.terminal .xterm-rows')?.firstElementChild?.textContent?.trim() ?? '')`
	termScrollbar = `document.querySelector('.terminal .xterm-scrollable-element > .scrollbar.vertical')`
)

// wheelOverTerminal turns the mouse wheel over the terminal's screen, as a
// person scrolling back would; negative deltaY scrolls up.
func wheelOverTerminal(deltaY float64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var at struct{ X, Y float64 }
		js := `(({x, y, width, height}) => ({X: x + width / 2, Y: y + height / 2}))(document.querySelector('.terminal .xterm-screen').getBoundingClientRect())`
		if err := chromedp.Evaluate(js, &at).Do(ctx); err != nil {
			return err
		}
		return input.DispatchMouseEvent(input.MouseWheel, at.X, at.Y).WithDeltaX(0).WithDeltaY(deltaY).Do(ctx)
	})
}

// scrollToTheTop wheels up, a notch at a time as a person would, until the
// oldest line is the first row, then lets xterm's smooth scroll settle.
func scrollToTheTop() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for i := 0; i < 60; i++ {
			var first string
			if err := chromedp.Evaluate(termFirstRow, &first).Do(ctx); err != nil {
				return err
			}
			if first == "old line 1" {
				return chromedp.Sleep(300 * time.Millisecond).Do(ctx)
			}
			if err := wheelOverTerminal(-500).Do(ctx); err != nil {
				return err
			}
			if err := chromedp.Sleep(50 * time.Millisecond).Do(ctx); err != nil {
				return err
			}
		}
		var first string
		_ = chromedp.Evaluate(termFirstRow, &first).Do(ctx)
		return fmt.Errorf("could not scroll to the oldest line; first row is %q", first)
	})
}

// visibleRows is each row's text, top to bottom, trimmed.
const visibleRows = `[...document.querySelectorAll('.terminal .xterm-rows > div')].map(r => r.textContent.trim())`

// pressWithShift presses a non-printing key with Shift held, through the
// browser's input pipeline so xterm's key handler sees a real event.
func pressWithShift(key string, virtualKey int64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		down := input.DispatchKeyEvent(input.KeyRawDown).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(virtualKey).WithModifiers(input.ModifierShift)
		if err := down.Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(virtualKey).WithModifiers(input.ModifierShift).Do(ctx)
	})
}

func attachWithHistory(t *testing.T) (context.Context, *errorLog, *historyPTY) {
	t.Helper()
	pty := newHistoryPTY()
	srv := terminalPage(t, pty)
	ctx, errs := browser(t)
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`document.querySelector('.terminal .bar .status')?.textContent === 'live'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// The pane's own size, redrawn by the PTY: the screen fills the viewport.
		chromedp.Poll(termFirstRow+` === 'live top'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	return ctx, errs, pty
}

// After attaching, the snapshot's scrollback is above the live screen: the
// screen shows as the server drew it, and scrolling up reaches the oldest
// line, which sits right above the newest history and the screen.
func TestTerminalHistoryFromTheSnapshotIsAboveTheScreen(t *testing.T) {
	ctx, errs, _ := attachWithHistory(t)

	var atTop []string
	err := chromedp.Run(ctx,
		scrollToTheTop(),
		chromedp.Evaluate(visibleRows, &atTop),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if len(atTop) < 2 || atTop[0] != "old line 1" || atTop[1] != "old line 2" {
		t.Errorf("scrolled to the top, the rows should start with the oldest history: %q", atTop)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// With history behind it, the pane shows a scrollbar at rest, in the design
// system's colour, so a viewer can see there is more above.
func TestTerminalScrollbarIsVisibleWhenThereIsHistory(t *testing.T) {
	ctx, errs, _ := attachWithHistory(t)

	var opacity string
	var hasHistoryHook bool
	var themed bool
	err := chromedp.Run(ctx,
		chromedp.Sleep(1200*time.Millisecond), // past xterm's fade-out
		chromedp.Evaluate(`getComputedStyle(`+termScrollbar+`).opacity`, &opacity),
		chromedp.Evaluate(`document.querySelector('.terminal .screen')?.hasAttribute('data-history') ?? false`, &hasHistoryHook),
		chromedp.Evaluate(`(() => {
			const paint = (css) => {
				const c = document.createElement('canvas'); c.width = c.height = 1;
				const x = c.getContext('2d'); x.fillStyle = '#010203'; x.fillStyle = css; x.fillRect(0, 0, 1, 1);
				return [...x.getImageData(0, 0, 1, 1).data].join(',');
			};
			const slider = `+termScrollbar+`.querySelector('.slider');
			const want = getComputedStyle(document.documentElement).getPropertyValue('--color-line-strong');
			return !!slider && paint(getComputedStyle(slider).backgroundColor) === paint(want);
		})()`, &themed),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if opacity != "1" {
		t.Errorf("scrollbar opacity at rest = %q, want 1 (visible)", opacity)
	}
	if !hasHistoryHook {
		t.Error("the screen element does not carry data-history while there is scrollback")
	}
	if !themed {
		t.Error("the scrollbar slider is not painted with --color-line-strong")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Output arriving while the viewer reads history does not pull the view down.
func TestTerminalStaysPutWhenOutputArrivesWhileScrolledUp(t *testing.T) {
	ctx, errs, pty := attachWithHistory(t)

	var before, after string
	err := chromedp.Run(ctx,
		scrollToTheTop(),
		chromedp.Evaluate(termFirstRow, &before),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for i := 0; i < 5; i++ {
		pty.print <- fmt.Sprintf("new output %d\r\n", i)
	}
	err = chromedp.Run(ctx,
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(termFirstRow, &after),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if before == "" || before != after {
		t.Errorf("first visible row moved from %q to %q while scrolled up", before, after)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Typing while scrolled up returns the view to the live screen, as terminal
// emulators do, and the key still reaches the PTY.
func TestTerminalTypingReturnsToTheLiveScreen(t *testing.T) {
	ctx, errs, pty := attachWithHistory(t)

	var rows string
	err := chromedp.Run(ctx,
		chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery),
		scrollToTheTop(),
		chromedp.KeyEvent("x"),
		chromedp.Poll(termRows+`.includes('$ x')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(termRows, &rows),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v\nrows: %q", err, errs.all(), rows)
	}
	if strings.Contains(rows, "old line") {
		t.Errorf("history still on screen after typing: %q", rows)
	}
	typed := ""
	for _, m := range pty.messages() {
		if m.Type == "key" && m.Key != nil {
			typed += m.Key.Text
		}
	}
	if typed != "x" {
		t.Errorf("keys the PTY received = %q, want x", typed)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Shift+PageUp scrolls the viewport a page and is not sent to the PTY;
// Shift+End comes back to the live screen.
func TestTerminalShiftPageKeysScrollWithoutReachingThePTY(t *testing.T) {
	ctx, errs, pty := attachWithHistory(t)

	var scrolledUp []string
	var backDown string
	err := chromedp.Run(ctx,
		chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery),
		pressWithShift("PageUp", 33),
		chromedp.Poll(termRows+`.includes('old line')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(visibleRows, &scrolledUp),
		pressWithShift("End", 35),
		chromedp.Poll(termFirstRow+` === 'live top'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(termRows, &backDown),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	// One page up: the newest history ends right where the screen begins.
	joined := strings.Join(scrolledUp, "|")
	if !strings.Contains(joined, "old line 60|live top") {
		t.Errorf("one page up should show the newest history right above the screen's first row, got %q", scrolledUp)
	}
	if strings.Contains(backDown, "old line") {
		t.Errorf("Shift+End did not return to the live screen: %q", backDown)
	}
	for _, m := range pty.messages() {
		if m.Type == "key" {
			t.Errorf("the PTY received a key while the viewer scrolled: %+v", m.Key)
		}
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
