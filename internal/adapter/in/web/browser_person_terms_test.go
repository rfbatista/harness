//go:build !windows

package web

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/adapter/out/personshell"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The person's terminals in the Files tab, over real shells on real PTYs: the
// dock under the viewer, opened from the tree's actions.

// passthroughDeny stands in for the platform's sandbox, which cannot nest in
// the agent sandbox: it runs the shell as it is.
type passthroughDeny struct{}

func (passthroughDeny) WrapDenyWrites(_ string, cmd ports.SandboxCommand) (ports.SandboxCommand, error) {
	return cmd, nil
}

// switchablePersons is the person's terminals behind the routes, which a test
// replaces as a harness restart would: the new ones list nothing.
type switchablePersons struct {
	mu    sync.Mutex
	cur   *vshell.PersonTerminals
	build func() *vshell.PersonTerminals
}

func newSwitchablePersons(t *testing.T, fsys *vfs.FileSystem, sess *domain.Session, events *vfs.RootEvents, deny ports.DenyWritesDriver) *switchablePersons {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	s := &switchablePersons{}
	s.build = func() *vshell.PersonTerminals {
		pty := ptyunix.New()
		host := termhost.New(shell.Direct{}, pty).WithLaunchers(personshell.New(pty, func() []string { return vshell.PersonEnv(os.Environ()) }, deny))
		svc := vshell.NewPersonTerminals(vshell.PersonDeps{Host: host, Folders: fsys, Sessions: filesSessions{s: sess}, RootEvents: events})
		t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
		return svc
	}
	s.cur = s.build()
	return s
}

func (s *switchablePersons) now() *vshell.PersonTerminals {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// restart is the harness restarting: a fresh service that knows no terminal.
func (s *switchablePersons) restart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur = s.build()
}

func (s *switchablePersons) Open(ctx context.Context, id string, r ports.PersonTerminalOpen) (ports.PersonTerminal, error) {
	return s.now().Open(ctx, id, r)
}
func (s *switchablePersons) List(ctx context.Context, id string) ([]ports.PersonTerminal, error) {
	return s.now().List(ctx, id)
}
func (s *switchablePersons) Attach(id string) (ports.Terminal, error) { return s.now().Attach(id) }
func (s *switchablePersons) Close(id string) error                    { return s.now().Close(id) }

const (
	dock       = `section[aria-label=Files] .person-terms`
	dockShown  = dock + ` .terminal:not([style*="display: none"])`
	dockScreen = dockShown + ` .xterm-rows`
)

func (fw *filesWorld) openTerminalHere(title string) chromedp.Action {
	return chromedp.Evaluate(`document.querySelector('`+filesTree+` [title="`+title+`"] .actions button').click()`, nil)
}

func (fw *filesWorld) waitDockLive() chromedp.Action {
	return chromedp.Poll(`!!document.querySelector('`+dockShown+` .xterm-rows') && document.querySelector('`+dockShown+`').dataset.state === 'live'`, nil, chromedp.WithPollingTimeout(15*time.Second))
}

func typeLine(line string) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.Focus(dockShown+` .xterm-helper-textarea`, chromedp.ByQuery),
		chromedp.KeyEvent(line),
		chromedp.KeyEvent("\r"),
	}
}

func waitScreen(want string) chromedp.Action {
	return chromedp.Poll(`(document.querySelector('`+dockScreen+`')?.textContent ?? '').includes(`+jsString(want)+`)`, nil, chromedp.WithPollingTimeout(15*time.Second))
}

func jsString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

func TestPersonTerminalOpensFromTheTreeSurvivesAReloadAndCloses(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	var badge, tabs, afterReload, tabsAfter string
	err := chromedp.Run(ctx,
		fw.openFilesTab(),
		fw.openTerminalHere("web"),
		fw.waitDockLive(),
	)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	err = chromedp.Run(ctx,
		typeLine(`echo here=$(pwd -P | sed 's|.*/||')`),
		waitScreen("here=web"),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.includes('Files')).querySelector('.badge')?.textContent.trim() ?? ''`, &badge),
		chromedp.Evaluate(`[...document.querySelectorAll('`+dock+` [role=tab] .label')].map(e => e.textContent).join(',')`, &tabs),
		// A reload: the page lists the terminals and reattaches, with the screen it had.
		chromedp.Navigate(fw.tab),
		chromedp.Poll(`!!document.querySelector('.pane-tabs [role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim().startsWith('Files')).click()`, nil),
		fw.waitDockLive(),
		waitScreen("here=web"),
		chromedp.Evaluate(`document.querySelector('`+dockScreen+`').textContent.includes('here=web')`, new(bool)),
		chromedp.Evaluate(`[...document.querySelectorAll('`+dock+` [role=tab] .label')].map(e => e.textContent).join(',')`, &tabsAfter),
		chromedp.Evaluate(`document.querySelector('`+dock+`').getAttribute('aria-label')`, &afterReload),
		// Close it: the tab and the dock go.
		chromedp.Click(dock+` .term-tab .close`, chromedp.ByQuery),
		chromedp.Poll(`getComputedStyle(document.querySelector('`+dock+`')).display === 'none'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	if badge != "1" || tabs != "web" || tabsAfter != "web" {
		t.Errorf("badge %q, tabs %q, after reload %q; want 1, web, web", badge, tabs, tabsAfter)
	}
	left, _ := fw.persons.List(context.Background(), "s1")
	for deadline := time.Now().Add(5 * time.Second); len(left) != 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		left, _ = fw.persons.List(context.Background(), "s1")
	}
	if len(left) != 0 {
		t.Errorf("the server still runs %+v after Close", left)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// gridProblems: tput's rows and columns are what the dock shows, and no row
// or column is cut.
const gridProblems = `(() => {
  const out = [];
  const term = document.querySelector('` + dockShown + `');
  const screen = term.querySelector(':scope > .screen');
  const cs = getComputedStyle(screen);
  const box = screen.getBoundingClientRect();
  const bottom = box.bottom - parseFloat(cs.paddingBottom), right = box.right - parseFloat(cs.paddingRight);
  const rows = term.querySelector('.xterm-rows');
  const last = rows.lastElementChild.getBoundingClientRect();
  if (last.bottom > bottom + 0.5) out.push('the last row ends at ' + last.bottom + ', the screen at ' + bottom);
  const text = term.querySelector('.xterm-screen').getBoundingClientRect();
  if (text.right > right + 0.5) out.push('the text ends at ' + text.right + ', past ' + right);
  const free = bottom - last.bottom;
  const rowH = last.height;
  if (free >= rowH) out.push('a whole row (' + rowH + 'px) is unused: ' + free + 'px free');
  return out.join('; ');
})()`

// sizeLine asks the shell for its size with tput, the way a program does. A
// function keeps the line short: a long one wraps in a narrow dock.
const sizeFn = `s() { n=$((n+1)); echo "S$n=$(env -u LINES -u COLUMNS tput lines)x$(env -u LINES -u COLUMNS tput cols)E"; }`

// gridMatches asks the shell its size for the nth time, and compares it with
// what the dock draws.
func gridMatches(t *testing.T, ctx context.Context, asked *int, what string) {
	t.Helper()
	*asked++
	var shown, problems string
	line := fmt.Sprintf(`S%d=(\d+)x(\d+)E`, *asked)
	err := chromedp.Run(ctx,
		chromedp.Sleep(600*time.Millisecond), // the refit settles
		typeLine(`s`),
		chromedp.Poll(`new RegExp(`+jsString(line)+`).test(document.querySelector('`+dockScreen+`').textContent)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(() => { const m = document.querySelector('`+dockScreen+`').textContent.match(new RegExp(`+jsString(line)+`)); return m[1] + 'x' + m[2] + ' vs ' + document.querySelector('`+dockScreen+`').children.length + ' rows' })()`, &shown),
		chromedp.Evaluate(gridProblems, &problems),
	)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	var lines, cols, rows int
	if _, e := fmt.Sscanf(shown, "%dx%d vs %d rows", &lines, &cols, &rows); e != nil || lines != rows {
		t.Errorf("%s: the shell has %s: tput lines and the rows the dock draws differ (%v)", what, shown, e)
	}
	if problems != "" {
		t.Errorf("%s: %s", what, problems)
	}
}

// drag moves the dock's top edge from where it is to y with the mouse.
func drag(toY float64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var x, y float64
		if err := chromedp.Evaluate(`(() => { const r = document.querySelector('`+dock+` > .separator').getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2] })()`, &[]*float64{&x, &y}).Do(ctx); err != nil {
			return err
		}
		steps := []*input.DispatchMouseEventParams{
			input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1),
			input.DispatchMouseEvent(input.MouseMoved, x, (y+toY)/2).WithButton(input.Left),
			input.DispatchMouseEvent(input.MouseMoved, x, toY).WithButton(input.Left),
			input.DispatchMouseEvent(input.MouseReleased, x, toY).WithButton(input.Left).WithClickCount(1),
		}
		for _, s := range steps {
			if err := s.Do(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

func TestPersonTerminalDockResizesAndExpandsWithAnExactFit(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	if err := chromedp.Run(ctx, fw.openFilesTab(), fw.openTerminalHere("Worktree"), fw.waitDockLive()); err != nil {
		failFiles(t, ctx, err, errs)
	}
	asked := 0
	if err := chromedp.Run(ctx, typeLine(sizeFn)); err != nil {
		t.Fatal(err)
	}
	gridMatches(t, ctx, &asked, "at first")

	var before, after float64
	var minH, maxH float64
	err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &before),
		drag(300),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &after)); err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Errorf("dragging the edge up left the dock at %.0f (was %.0f)", after, before)
	}
	gridMatches(t, ctx, &asked, "after dragging taller")

	// Keys on the handle, within the limits.
	var now0, now1 string
	err = chromedp.Run(ctx,
		chromedp.Focus(dock+` > .separator`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('`+dock+` > .separator').getAttribute('aria-valuenow')`, &now0),
		chromedp.KeyEvent(kb.ArrowDown),
		chromedp.KeyEvent(kb.ArrowDown),
		chromedp.Evaluate(`document.querySelector('`+dock+` > .separator').getAttribute('aria-valuenow')`, &now1),
		chromedp.KeyEvent(kb.Home),
		chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &minH),
		chromedp.KeyEvent(kb.End),
		chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &maxH),
	)
	if err != nil {
		t.Fatal(err)
	}
	if now0 == now1 || minH < 100 || minH > 130 || maxH < 500 {
		t.Errorf("handle: value %s then %s, Home %.0fpx, End %.0fpx", now0, now1, minH, maxH)
	}
	gridMatches(t, ctx, &asked, "at the tallest")

	// Expanded: the tree and the viewer hide, the dock fills the tab; Escape restores.
	var treeHidden bool
	var expandedH, paneH float64
	err = chromedp.Run(ctx,
		chromedp.Click(dock+` .tools button[aria-label="Expand terminals"]`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('section[aria-label=Files] .code-layout > .side')).display === 'none'`, &treeHidden),
		chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &expandedH),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Files] .code-layout').getBoundingClientRect().height`, &paneH),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !treeHidden || expandedH < paneH-2 {
		t.Errorf("expanded: tree hidden %v, dock %.0f of %.0f", treeHidden, expandedH, paneH)
	}
	gridMatches(t, ctx, &asked, "expanded")

	var restoredTree bool
	err = chromedp.Run(ctx,
		// Escape from the dock's own button: in the shell it would be the shell's key.
		chromedp.Focus(dock+` .tools button[aria-label="Restore terminals"]`, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('section[aria-label=Files] .code-layout > .side')).display !== 'none'`, &restoredTree),
	)
	if err != nil || !restoredTree {
		t.Errorf("Escape did not restore the tree: %v %v", restoredTree, err)
	}
	gridMatches(t, ctx, &asked, "restored")

	// The height outlives a reload.
	var kept float64
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &before),
		chromedp.Navigate(fw.tab),
		chromedp.Poll(`!!document.querySelector('.pane-tabs [role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim().startsWith('Files')).click()`, nil),
		fw.waitDockLive(),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &kept),
	)
	if err != nil {
		t.Fatal(err)
	}
	if kept < before-2 || kept > before+2 {
		t.Errorf("the dock was %.0fpx and came back at %.0fpx", before, kept)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

func TestPersonTerminalOnTheResourcesRootIsReadOnlyOrRefused(t *testing.T) {
	// With a sandbox: the terminal opens, with its note.
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	var note string
	if err := chromedp.Run(ctx, fw.openFilesTab(), fw.openTerminalHere("Resources"), fw.waitDockLive(),
		chromedp.Evaluate(`document.querySelector('`+dockShown+` .pane-note')?.textContent ?? ''`, &note)); err != nil {
		failFiles(t, ctx, err, errs)
	}
	if !strings.Contains(note, "read-only") {
		t.Errorf("the resources terminal's note = %q", note)
	}

	// Without one: the refusal takes the terminal's place.
	fw2 := newFilesWorldWith(t, domain.SessionRunning, 0, nil)
	ctx2, errs2 := browser(t)
	var title, code string
	if err := chromedp.Run(ctx2, fw2.openFilesTab(), fw2.openTerminalHere("Resources"),
		chromedp.Poll(`!!document.querySelector('`+dock+` .term-refused')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+dock+` .term-refused strong').textContent`, &title),
		chromedp.Evaluate(`document.querySelector('`+dock+` .term-refused code').textContent`, &code)); err != nil {
		failFiles(t, ctx2, err, errs2)
	}
	if code != "TERMINAL_READ_ONLY_UNAVAILABLE" || !strings.Contains(title, "cannot be made read-only") {
		t.Errorf("refusal = %q %q", title, code)
	}
	// A worktree terminal still opens, and the refusal goes away.
	if err := chromedp.Run(ctx2, fw2.openTerminalHere("Worktree"), fw2.waitDockLive(),
		chromedp.Poll(`!document.querySelector('`+dock+` .term-refused')`, nil, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		failFiles(t, ctx2, err, errs2)
	}
}

func TestPersonTerminalNotesForAnEndedShellAndAHarnessRestart(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	var exited, restarted string
	err := chromedp.Run(ctx,
		fw.openFilesTab(),
		fw.openTerminalHere("Worktree"),
		fw.waitDockLive(),
		typeLine("exit 0"),
		chromedp.Poll(`!!document.querySelector('`+dockShown+` .pane-note button')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+dockShown+` .pane-note strong').textContent`, &exited),
		// A new terminal in the same slot.
		chromedp.Click(dockShown+` .pane-note button`, chromedp.ByQuery),
		fw.waitDockLive(),
		chromedp.Evaluate(`document.querySelectorAll('`+dock+` [role=tab]').length`, new(int)),
	)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	if exited != "The shell exited." {
		t.Errorf("exit note = %q", exited)
	}

	// The harness restarts: the connection drops, the new one lists nothing, and the page
	// says so when it reloads the list (as its feed does on resync).
	fw.persons.restart()
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('`+dock+`')._x_dataStack[0].load()`, nil),
		chromedp.Poll(`document.querySelector('`+dockShown+` .pane-note strong')?.textContent.includes('restarted')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+dockShown+` .pane-note strong').textContent`, &restarted),
	)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	if restarted != "The harness restarted; this terminal ended." {
		t.Errorf("restart note = %q", restarted)
	}
}

// With the session page's focus mode on, the expanded dock fills almost the
// whole window.
func TestPersonTerminalExpandedInFocusModeFillsTheWindow(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	var dockH, inner float64
	err := chromedp.Run(ctx,
		fw.openFilesTab(),
		fw.openTerminalHere("Worktree"),
		fw.waitDockLive(),
		// Focus mode is the Agent tab's tool; it stays on when the person moves to Files.
		// Focus mode is chosen on the Agent tab; with the dock expanded it applies on Files too.
		chromedp.Evaluate(`document.querySelector('main#main')._x_dataStack[0].toggleFocus()`, nil),
		chromedp.Click(dock+` .tools button[aria-label="Expand terminals"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('.app-shell')?.hasAttribute('data-focus')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('`+dock+`').getBoundingClientRect().height`, &dockH),
		chromedp.Evaluate(`innerHeight`, &inner),
	)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	if dockH < inner*0.75 {
		t.Errorf("the expanded dock is %.0fpx of a %.0fpx window", dockH, inner)
	}
}

// TestPersonTerminalScreenshots saves the dock's states for the design
// review when WEB_SCREENSHOT_DIR is set; it checks nothing else.
func TestPersonTerminalScreenshots(t *testing.T) {
	if os.Getenv("WEB_SCREENSHOT_DIR") == "" {
		t.Skip("set WEB_SCREENSHOT_DIR to save the dock's screenshots")
	}
	shot := func(ctx context.Context, name string) {
		t.Helper()
		var b []byte
		if err := chromedp.Run(ctx, chromedp.Sleep(700*time.Millisecond), chromedp.CaptureScreenshot(&b)); err != nil {
			t.Fatal(err)
		}
		saveShot(name, b)
	}
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	err := chromedp.Run(ctx,
		fw.openFilesTab(),
		fw.openTerminalHere("Worktree"), fw.waitDockLive(),
		typeLine("echo hello from the worktree; pwd"), waitScreen("hello from the worktree"),
		fw.openTerminalHere("Resources"),
		chromedp.Poll(`document.querySelectorAll('`+dock+` [role=tab]').length === 2 && document.querySelector('`+dockShown+`')?.dataset.state === 'live'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	shot(ctx, "person-terms-wide-two-terminals.png")

	// The resources shell after its session ended.
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('`+dock+`')._x_dataStack[0].setSessionEnded(true)`, nil))
	shot(ctx, "person-terms-root-gone-note.png")

	// A harness restart, then a refusal.
	fw.persons.restart()
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const d = document.querySelector('`+dock+`')._x_dataStack[0]; d.sessionEnded = false; d.select(d.terms[0].id); return d.load() })()`, nil),
		chromedp.Poll(`document.querySelector('`+dockShown+` .pane-note strong')?.textContent.includes('restarted')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	)
	shot(ctx, "person-terms-restart-note.png")

	fw2 := newFilesWorldWith(t, domain.SessionRunning, 0, nil)
	ctx2, errs2 := browser(t)
	if err := chromedp.Run(ctx2, fw2.openFilesTab(), fw2.openTerminalHere("Worktree"), fw2.waitDockLive(), fw2.openTerminalHere("Resources"),
		chromedp.Poll(`!!document.querySelector('`+dock+` .term-refused')`, nil, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		failFiles(t, ctx2, err, errs2)
	}
	shot(ctx2, "person-terms-refused.png")

	// Expanded, and on a phone.
	if err := chromedp.Run(ctx2, chromedp.Click(dock+` .tools button[aria-label="Expand terminals"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	shot(ctx2, "person-terms-expanded.png")
	if err := chromedp.Run(ctx2, chromedp.Click(dock+` .tools button[aria-label="Restore terminals"]`, chromedp.ByQuery), chromedp.EmulateViewport(390, 800), chromedp.Sleep(500*time.Millisecond),
		// On a phone the tree and the terminal take turns: Open terminal here shows the terminal.
		chromedp.Evaluate(`document.querySelector('`+filesTree+` [title="web"] .actions button').click()`, nil),
		chromedp.Poll(`document.querySelectorAll('`+dock+` [role=tab]').length === 2 && getComputedStyle(document.querySelector('`+dock+`')).display !== 'none' && document.querySelector('`+dock+`').getBoundingClientRect().height > 100`, nil, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		failFiles(t, ctx2, err, errs2)
	}
	shot(ctx2, "person-terms-phone.png")
}

// Closing the last terminal of an expanded dock gives the Files tab back: the
// tree and the viewer show, focus mode ends, and a stored "expanded" flag does
// not blank a page that has no terminal.
func TestClosingTheLastExpandedTerminalGivesTheFilesTabBack(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	const tabShows = `(() => {
  const vis = (q) => { const e = document.querySelector('section[aria-label=Files] ' + q); return !!e && getComputedStyle(e).display !== 'none' && e.getBoundingClientRect().height > 0 };
  return JSON.stringify({ tree: vis('.code-layout > .side'), viewer: vis('.code-layout > .main > .code-view, .code-layout > .main > .placeholder, .code-layout > .main [aria-label=File]') || vis('.code-layout > .main'), strip: vis('.files-strip'), focus: document.querySelector('.app-shell').hasAttribute('data-focus') })
})()`
	var after, reloaded, stale string
	var shot []byte
	err := chromedp.Run(ctx,
		fw.openFilesTab(),
		fw.openTerminalHere("Worktree"),
		fw.waitDockLive(),
		chromedp.Evaluate(`document.querySelector('main#main')._x_dataStack[0].toggleFocus()`, nil),
		chromedp.Click(dock+` .tools button[aria-label="Expand terminals"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('.app-shell').hasAttribute('data-focus')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(dock+` .term-tab .close`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(tabShows, &after),
		chromedp.CaptureScreenshot(&shot),
		// A reload with no terminal left.
		chromedp.Navigate(fw.tab),
		chromedp.Poll(`!!document.querySelector('.pane-tabs [role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim().startsWith('Files')).click()`, nil),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Files] .file-tree [data-kind=root]').length === 2`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(tabShows, &reloaded),
		// A stored flag from an earlier page, and no terminal: the tab is not blank.
		chromedp.Evaluate(`localStorage.setItem('harness:person-terms.expanded', '1')`, nil),
		chromedp.Navigate(fw.tab),
		chromedp.Poll(`!!document.querySelector('.pane-tabs [role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim().startsWith('Files')).click()`, nil),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Files] .file-tree [data-kind=root]').length === 2`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(tabShows, &stale),
	)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	saveShot("person-terms-closed-after-expanded.png", shot)
	want := `{"tree":true,"viewer":true,"strip":true,"focus":false}`
	for what, got := range map[string]string{"after closing": after, "after a reload": reloaded, "with a stale flag": stale} {
		if got != want {
			t.Errorf("%s the Files tab shows %s, want %s", what, got, want)
		}
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
