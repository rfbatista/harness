package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// Rail-island stories the other rail browser tests leave open: the browser
// draws the rail when the server cannot (3), in-place navigation moves the
// current page and the open task without a reload (5, 6), and the drawer
// closes from its Close button and from navigation (17).

// newFailingRenderRailHarness is newRailHarness with a renderer that always
// fails, so the server sends empty marked mounts.
func newFailingRenderRailHarness(t *testing.T) *railHarness {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", http.NotFoundHandler())
	deps := railWorld()
	deps.IslandRenderer = failingRenderer{}
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &railHarness{ctx: ctx, srv: srv, feed: feed, errs: errs}
}

// Story 3: with server rendering failing, the rail and the Tasks button are
// empty on the server's page and the browser draws both.
func TestAFailedServerRenderLeavesTheRailToTheBrowser(t *testing.T) {
	h := newFailingRenderRailHarness(t)
	var marked int
	var drawn bool
	h.run(t, "the server's page",
		chromedp.EmulateViewport(1280, 900),
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`document.querySelectorAll('[data-island-rendered="false"]').length`, &marked),
		chromedp.Evaluate(`!!document.querySelector('nav[aria-label="Tasks"] a[data-task-id]')`, &drawn),
		emulation.SetScriptExecutionDisabled(false),
	)
	if marked < 2 || drawn {
		t.Errorf("a failed render leaves the rail and Tasks button as marked empty mounts: marked %d, rows drawn %v", marked, drawn)
	}
	var rows, current, groups string
	h.run(t, "browser draws the rail",
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`[...document.querySelectorAll('nav[aria-label="Tasks"] section')].map(s => s.dataset.key).join(',')`, &groups),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] a[aria-current="page"]').dataset.taskId`, &current),
		chromedp.Evaluate(shownRows("in_progress"), &rows),
	)
	if groups == "" || current != "t-cur" || rows != "t-cur t-rev t-run" {
		t.Errorf("client-drawn rail: groups %q, open task %q, In progress %q", groups, current, rows)
	}
	var toggle string
	h.run(t, "browser draws the tasks button",
		chromedp.Evaluate(`document.querySelector('.tasks-toggle .badge').textContent.trim()`, &toggle),
	)
	if toggle != "2 need you" {
		t.Errorf("Tasks button badge = %q, want \"2 need you\"", toggle)
	}
	h.noErrors(t)
}

// Stories 5 and 6: following a link in place marks the new page and the new
// open task current, with no reload (a flag on window survives).
func TestRailFollowsInPlaceNavigation(t *testing.T) {
	h := newRailHarness(t)
	var flag bool
	var path, current, openTask string
	h.run(t, "navigate in place",
		chromedp.EmulateViewport(1280, 900),
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.Evaluate(`window.__kept = true`, nil),
		chromedp.Click(`nav[aria-label="Tasks"] a[data-task-id=t-viz]`, chromedp.ByQuery),
		railPoll(`location.pathname === '/projects/p1/tasks/t-viz' && document.querySelector('nav[aria-label="Tasks"] a[aria-current="page"]')?.dataset.taskId === 't-viz'`),
		chromedp.Evaluate(`window.__kept === true`, &flag),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`document.querySelectorAll('nav[aria-label="Tasks"] a[aria-current="page"]').length + ':' + document.querySelector('nav[aria-label="Tasks"] a[aria-current="page"]').dataset.taskId`, &current),
	)
	if !flag || path != "/projects/p1/tasks/t-viz" || current != "1:t-viz" {
		t.Errorf("after the click: no reload %v, path %q, current rows %q", flag, path, current)
	}
	// Going to the Board marks the nav item and leaves no task current.
	var navCurrent string
	h.run(t, "to the board",
		chromedp.Click(`.rail a[href="/projects/p1"]`, chromedp.ByQuery),
		railPoll(`location.pathname === '/projects/p1' &&!document.querySelector('nav[aria-label="Tasks"] a[aria-current="page"]')`),
		chromedp.Evaluate(`[...document.querySelectorAll('.rail a[aria-current="page"]')].map(a => a.getAttribute('href')).join(',')`, &navCurrent),
		chromedp.Evaluate(`window.__kept === true`, &flag),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] a[aria-current="page"]')?.dataset.taskId ?? ''`, &openTask),
	)
	if !flag || openTask != "" || navCurrent != "/projects/p1" {
		t.Errorf("on the board: no reload %v, open task %q, current links %q", flag, openTask, navCurrent)
	}
	h.noErrors(t)
}

// Story 17: below 48rem the drawer closes from its Close button and from any
// in-place navigation, and the Tasks button says so.
func TestRailDrawerClosesFromCloseAndFromNavigation(t *testing.T) {
	h := newRailHarness(t)
	var expanded, afterClose, afterNav string
	h.run(t, "close button",
		chromedp.EmulateViewport(375, 740),
		h.openLive("/projects/p1/tasks/t-cur"),
		chromedp.Click(`.tasks-toggle`, chromedp.ByQuery),
		railPoll(`document.querySelector('.rail').hasAttribute('data-open')`),
		chromedp.Sleep(900*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.tasks-toggle').getAttribute('aria-expanded')`, &expanded),
		chromedp.Click(`.rail .drawer-head button[aria-label=Close]`, chromedp.ByQuery),
		railPoll(`!document.querySelector('.rail').hasAttribute('data-open')`),
		chromedp.Evaluate(`document.querySelector('.tasks-toggle').getAttribute('aria-expanded') + ':' + (document.activeElement.classList.contains('tasks-toggle') ? 'toggle' : document.activeElement.tagName)`, &afterClose),
	)
	if expanded != "true" || afterClose != "false:toggle" {
		t.Errorf("open expanded %q; after Close %q, want false:toggle", expanded, afterClose)
	}
	h.run(t, "navigation closes it",
		chromedp.Sleep(900*time.Millisecond), // the slide out
		chromedp.Click(`.tasks-toggle`, chromedp.ByQuery),
		railPoll(`document.querySelector('.rail').hasAttribute('data-open')`),
		chromedp.Sleep(900*time.Millisecond),
		chromedp.Click(`nav[aria-label="Tasks"] a[data-task-id=t-viz]`, chromedp.ByQuery),
		railPoll(`location.pathname === '/projects/p1/tasks/t-viz' && !document.querySelector('.rail').hasAttribute('data-open')`),
		chromedp.Evaluate(`document.querySelector('.tasks-toggle').getAttribute('aria-expanded')`, &afterNav),
	)
	if afterNav != "false" {
		t.Errorf("after in-place navigation the Tasks button says expanded=%q, want false", afterNav)
	}
	h.noErrors(t)
}
