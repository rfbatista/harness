package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The session terminal fits its pane (the layout contract, rules L1–L6): the
// page is a viewport-height layout, the terminal takes the height between the
// tab bar and its footer, xterm's last row and last column are inside the
// pane, the page never scrolls, and each size change sends one resize, never
// one for a hidden pane. A task with many sessions is the case that broke it:
// the list made the page taller than the window.

// crowdedTerminalPage serves the task page with session s1's PTY handled by
// pty and 40 finished sessions under it, a list taller than any window.
func crowdedTerminalPage(t *testing.T, pty http.Handler) *httptest.Server {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	sessions := []*domain.Session{{
		ID: "s1", ProjectID: "p1", TicketID: "t1", Task: "implement this task",
		Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now(),
	}}
	for i := range 40 {
		sessions = append(sessions, &domain.Session{
			ID: fmt.Sprintf("done%d", i), ProjectID: "p1", TicketID: "t1", Task: fmt.Sprintf("earlier session %d", i),
			Status: domain.SessionDone, UpdatedAt: time.Now().Add(-time.Hour),
		})
	}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions:     fakeSessions{sessions},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/terminal", pty)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := httptest.NewServer(withStream(mux))
	t.Cleanup(srv.Close)
	return srv
}

// paneProblems lists every way the terminal under scope breaks the layout
// contract, empty when it fits: the last row below the screen's content box
// (L3), the text past the content box or under xterm's scrollbar (L3), the
// terminal ending below the stream bar or the window (L1, L2), a footer bar
// left in it, its tools missing from the tab row (L7), and any scrolling box
// between the document and the terminal (L1, L5).
func paneProblems(scope string) string {
	return `((scope) => {
  const out = [];
  const term = document.querySelector(scope + ' .terminal');
  if (!term) return ['no terminal under ' + scope];
  const screen = term.querySelector(':scope > .screen');
  const cs = getComputedStyle(screen);
  const box = screen.getBoundingClientRect();
  const content = {
    bottom: box.bottom - parseFloat(cs.paddingBottom) - parseFloat(cs.borderBottomWidth),
    right: box.right - parseFloat(cs.paddingRight) - parseFloat(cs.borderRightWidth),
  };
  const last = term.querySelector('.xterm-rows')?.lastElementChild?.getBoundingClientRect();
  if (!last) out.push('no rows drawn');
  else if (last.bottom > content.bottom + 0.5) out.push('last row ends at ' + last.bottom + ', the screen at ' + content.bottom);
  const text = term.querySelector('.xterm-screen').getBoundingClientRect();
  const scrollbar = term.querySelector('.xterm-scrollable-element > .scrollbar.vertical')?.getBoundingClientRect();
  const right = scrollbar && scrollbar.width ? Math.min(content.right, scrollbar.left) : content.right;
  if (text.right > right + 0.5) out.push('text ends at x=' + text.right + ', past ' + right);
  const pane = term.getBoundingClientRect();
  const stream = document.querySelector('.stream-bar').getBoundingClientRect();
  if (pane.bottom > stream.top + 0.5) out.push('terminal ends at ' + pane.bottom + ', below the stream bar at ' + stream.top);
  if (pane.bottom > innerHeight + 0.5) out.push('terminal ends at ' + pane.bottom + ', below the window at ' + innerHeight);
  if (term.querySelector(':scope > .bar')) out.push('the terminal still has a footer bar');
  const tabs = document.querySelector('section[aria-label="Session detail"] .pane-tabs');
  if (!tabs?.querySelector('.tools [role=status]')) out.push('no connection status in the tab row');
  else if (pane.top + 0.5 < tabs.getBoundingClientRect().bottom) out.push('terminal starts at ' + pane.top + ', above the tab row\'s end at ' + tabs.getBoundingClientRect().bottom);
  for (let el = term; el; el = el.parentElement) {
    if (el.scrollHeight > el.clientHeight + 1) out.push((el.className || el.tagName).toString().trim() + ' scrolls: ' + el.scrollHeight + ' > ' + el.clientHeight);
  }
  const doc = document.scrollingElement;
  if (doc.scrollHeight > doc.clientHeight) out.push('the document scrolls: ' + doc.scrollHeight + ' > ' + doc.clientHeight);
  return out;
})(` + "`" + scope + "`" + `).join('\n')`
}

// xtermRows is how many rows the session terminal shows.
const xtermRows = `document.querySelector('.terminal .xterm-rows')?.children.length ?? 0`

// resizes is every size the browser sent the PTY, in order.
func resizes(pty *historyPTY) []ports.TermSize {
	var out []ports.TermSize
	for _, m := range pty.messages() {
		if m.Type == "resize" && m.Size != nil {
			out = append(out, *m.Size)
		}
	}
	return out
}

// settle waits out the resize debounce and the PTY's redraw.
func settle() chromedp.Action { return chromedp.Sleep(600 * time.Millisecond) }

// checkFits fails the test with the pane's problems, and when the last size
// sent is not the size on screen.
func checkFits(t *testing.T, ctx context.Context, pty *historyPTY, errs *errorLog, when string) {
	t.Helper()
	var problems string
	var rows int
	if err := chromedp.Run(ctx, chromedp.Evaluate(paneProblems(`section[aria-label="Session detail"]`), &problems), chromedp.Evaluate(xtermRows, &rows)); err != nil {
		t.Fatalf("%s: %v\nJS errors: %v", when, err, errs.all())
	}
	if problems != "" {
		t.Errorf("%s: the terminal does not fit its pane:\n%s", when, problems)
	}
	sent := resizes(pty)
	if len(sent) == 0 {
		t.Errorf("%s: the browser never sent its size", when)
	} else if sent[len(sent)-1].Rows != rows {
		t.Errorf("%s: last resize sent %+v, the screen shows %d rows", when, sent[len(sent)-1], rows)
	}
}

func shoot(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	dir := os.Getenv("WEB_SCREENSHOT_DIR")
	if dir == "" {
		return
	}
	var shot []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&shot)); err == nil {
		_ = os.WriteFile(dir+"/"+name+".png", shot, 0o644)
	}
}

func openCrowdedTask(t *testing.T, width, height int64, scheme string) (context.Context, *errorLog, *historyPTY) {
	t.Helper()
	pty := newHistoryPTY()
	srv := crowdedTerminalPage(t, pty)
	ctx, errs := browser(t)
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		emulateColorScheme(scheme),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(10*time.Second)),
		settle(),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	return ctx, errs, pty
}

func TestSessionTerminalFitsItsPaneOnTheDesktop(t *testing.T) {
	for _, c := range []struct {
		width, height int64
		scheme        string
	}{{1280, 800, "dark"}, {1600, 1000, "light"}} {
		t.Run(fmt.Sprintf("%dx%d %s", c.width, c.height, c.scheme), func(t *testing.T) {
			ctx, errs, pty := openCrowdedTask(t, c.width, c.height, c.scheme)
			checkFits(t, ctx, pty, errs, "after attaching")
			// The list scrolls on its own; its rows keep their height.
			var squashed []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('[role=listbox] .row')].filter(r => r.scrollHeight > r.clientHeight + 1).map(r => r.textContent.trim().replace(/\s+/g, ' ') + ': ' + r.clientHeight + ' < ' + r.scrollHeight)`, &squashed)); err != nil {
				t.Fatal(err)
			}
			if len(squashed) > 0 {
				t.Errorf("%d list rows squashed to less than their content, e.g. %s", len(squashed), squashed[0])
			}
			shoot(t, ctx, fmt.Sprintf("terminal-%dx%d-%s", c.width, c.height, c.scheme))
			if e := errs.all(); len(e) > 0 {
				t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
			}
		})
	}
}

// A text size, focus mode on and off, and the window resizing each refit the
// terminal and send one resize (L4). The head has a fixed height: it never
// sends one.
func TestSessionTerminalRefitsOnceForEachPaneChange(t *testing.T) {
	ctx, errs, pty := openCrowdedTask(t, 1280, 800, "dark")
	checkFits(t, ctx, pty, errs, "after attaching")

	step := func(when string, action chromedp.Action) {
		t.Helper()
		before := len(resizes(pty))
		if err := chromedp.Run(ctx, action, settle()); err != nil {
			t.Fatalf("%s: %v\nJS errors: %v", when, err, errs.all())
		}
		if n := len(resizes(pty)) - before; n != 1 {
			t.Errorf("%s: %d resizes sent, want exactly one: %+v", when, n, resizes(pty)[before:])
		}
		checkFits(t, ctx, pty, errs, when)
	}
	pickTextSize := func(label string) chromedp.Action {
		return chromedp.Tasks{
			chromedp.Evaluate(`document.querySelector('.pane-tabs .menu.pref > button').click()`, nil),
			clickButton(`.pane-tabs .menu.pref [role=menu]`, label),
		}
	}
	focusButton := `document.querySelector('.pane-tabs .tools .button.pref[aria-pressed]')`
	step("text larger", pickTextSize("Larger"))
	step("text back to default", pickTextSize("Default"))
	step("focus mode on", chromedp.Evaluate(focusButton+`.click()`, nil))
	var hidden []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`['.rail', 'main .toolbar', '[role=listbox][aria-label="Sessions of this task"]', '.session-head'].filter(s => { const el = document.querySelector(s); return !el || el.checkVisibility(); })`, &hidden)); err != nil {
		t.Fatal(err)
	}
	if len(hidden) > 0 {
		t.Errorf("focus mode left these shown (or missing): %v", hidden)
	}
	var stillFocused bool
	if err := chromedp.Run(ctx, chromedp.Focus(`.terminal .xterm-helper-textarea`, chromedp.ByQuery), chromedp.KeyEvent(kb.Escape), settle(),
		chromedp.Evaluate(`document.querySelector('.app-shell').hasAttribute('data-focus')`, &stillFocused)); err != nil {
		t.Fatal(err)
	}
	if !stillFocused {
		t.Error("Esc left focus mode: it belongs to the agent")
	}
	step("focus mode off", chromedp.Evaluate(focusButton+`.click()`, nil))
	step("window grown", chromedp.EmulateViewport(1600, 1000))
	step("window shrunk", chromedp.EmulateViewport(1280, 800))
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Leaving the Agent tab sends nothing; coming back refits the new terminal.
func TestSessionTerminalRefitsWhenItsTabComesBack(t *testing.T) {
	ctx, errs, pty := openCrowdedTask(t, 1280, 800, "dark")
	before := len(resizes(pty))
	err := chromedp.Run(ctx,
		clickButton(`[role=tablist]`, "App"),
		chromedp.Poll(`!document.querySelector('.terminal')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		settle(),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if n := len(resizes(pty)) - before; n != 0 {
		t.Errorf("%d resizes sent while the terminal was not shown", n)
	}
	err = chromedp.Run(ctx,
		clickButton(`[role=tablist]`, "Agent"),
		chromedp.Poll(terminalLive, nil, chromedp.WithPollingTimeout(5*time.Second)),
		settle(),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	checkFits(t, ctx, pty, errs, "back on the Agent tab")
}

// Below 64rem the list is the page; the terminal attached behind it sends no
// size until the person opens the session. Then the session fills the page,
// with a way back to the list. J and K walk the list and never open it.
func TestSessionTerminalOnANarrowScreen(t *testing.T) {
	for _, c := range []struct{ width, height int64 }{{1000, 700}, {375, 700}, {320, 640}} {
		t.Run(fmt.Sprintf("%dx%d", c.width, c.height), func(t *testing.T) {
			ctx, errs, pty := openCrowdedTask(t, c.width, c.height, "dark")
			if sent := resizes(pty); len(sent) != 0 {
				t.Errorf("sizes sent for a terminal nobody can see: %+v", sent)
			}
			detailShown := `!!document.querySelector('section[aria-label="Session detail"]')?.checkVisibility()`
			listShown := `!!document.querySelector('[role=listbox][aria-label="Sessions of this task"]')?.checkVisibility()`
			var shown bool
			err := chromedp.Run(ctx,
				chromedp.Focus(`[role=listbox][aria-label="Sessions of this task"]`, chromedp.ByQuery),
				chromedp.KeyEvent("j"),
				chromedp.KeyEvent("k"),
				settle(),
				chromedp.Evaluate(detailShown, &shown),
			)
			if err != nil {
				t.Fatalf("%v\nJS errors: %v", err, errs.all())
			}
			if shown {
				t.Error("J/K opened the session; they should only move the selection")
			}

			var focusInTerminal bool
			err = chromedp.Run(ctx,
				chromedp.Evaluate(`document.querySelector('[role=option][data-session-id="s1"]').click()`, nil),
				chromedp.Poll(detailShown, nil, chromedp.WithPollingTimeout(5*time.Second)),
				settle(),
				chromedp.Evaluate(`!!document.activeElement?.closest('.terminal')`, &focusInTerminal),
			)
			if err != nil {
				t.Fatalf("%v\nJS errors: %v", err, errs.all())
			}
			if focusInTerminal {
				t.Error("opening the session focused its terminal")
			}
			if c.width < 768 {
				// A phone: the session's own head leads, with its back button.
				var shown []string
				if err := chromedp.Run(ctx, chromedp.Evaluate(`['.topbar', 'main .toolbar'].filter(s => document.querySelector(s)?.checkVisibility())`, &shown)); err != nil {
					t.Fatal(err)
				}
				if len(shown) > 0 {
					t.Errorf("with a session open on a phone these still show: %v", shown)
				}
			}
			checkFits(t, ctx, pty, errs, "session opened")
			shoot(t, ctx, fmt.Sprintf("terminal-narrow-%dx%d", c.width, c.height))

			var listBack, detailGone bool
			err = chromedp.Run(ctx,
				chromedp.Evaluate(`document.querySelector('section[aria-label="Session detail"] [data-split-back]').click()`, nil),
				chromedp.Poll(listShown, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Evaluate(listShown, &listBack),
				chromedp.Evaluate(`!(`+detailShown+`)`, &detailGone),
			)
			if err != nil {
				t.Fatalf("back to the list: %v\nJS errors: %v", err, errs.all())
			}
			if !listBack || !detailGone {
				t.Errorf("after Sessions: list shown %v, detail hidden %v", listBack, detailGone)
			}
			if e := errs.all(); len(e) > 0 {
				t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
			}
		})
	}
}
