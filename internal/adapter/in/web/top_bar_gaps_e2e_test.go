package web

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/web/serversettings"
)

// Gaps of the top bar island's end-to-end tests against the user stories of
// "Spec: Harness server — React top bar island": the picker without
// JavaScript, the attention mark across navigations, Back and Forward across
// an app shell swap, and the page when the bundles are not built.

const (
	// isolationMark says whether the Agent isolation link carries the attention mark.
	isolationMark = `document.querySelector('.topbar a[aria-label="Agent isolation"]')?.getAttribute('data-blocking') === 'true'`
	isolationLink = `!!document.querySelector('.topbar a[aria-label="Agent isolation"]')`
)

func (h *inboxHarness) waitJS(t *testing.T, what, js string) {
	t.Helper()
	h.run(t, what, chromedp.Poll(js, nil, chromedp.WithPollingTimeout(8*time.Second)))
}

// isolationHarness is the navigation harness with agents blocked until the
// returned credential is set.
func isolationHarness(t *testing.T) (*inboxHarness, *fakeAgentCredential) {
	t.Helper()
	cred := &fakeAgentCredential{}
	bin := &fakeClaudeBin{}
	bin.installed.Store(true)
	h := newInboxHarnessWith(t, inboxWorld(), func(d *Deps) {
		d.ProjectSummaries = fakeSummaries{}
		d.DataPaths = &serversettings.Paths{}
		d.SandboxSettings = fakeSandboxSwitch{}
		d.AgentCredential = cred
		d.ManagedClaudeBin = bin
	})
	return h, cred
}

func TestThePickerSwitchesProjectsWithoutJavaScriptThroughTheNoscriptForm(t *testing.T) {
	h := navigationHarness(t)
	var open, path string
	h.run(t, "without script",
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/projects"),
		chromedp.Evaluate(`document.querySelector('.topbar form[action="/switch-project"]')?.method ?? ''`, &open),
	)
	if open != "get" {
		t.Fatalf("the picker must sit in a GET form to /switch-project, method %q", open)
	}
	h.run(t, "choose p2 and press Open",
		chromedp.Evaluate(`document.querySelector('.topbar #project-picker').value = 'p2'; true`, nil),
		chromedp.Evaluate(`document.querySelector('.topbar form[action="/switch-project"] button').click(); true`, nil),
	)
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if err := chromedp.Run(h.ctx, chromedp.Evaluate(`location.pathname`, &path)); err == nil && path == "/projects/p2" {
			break
		}
	}
	var picker string
	h.run(t, "the new page", chromedp.Evaluate(barPicker, &picker), emulation.SetScriptExecutionDisabled(false))
	if path != "/projects/p2" || picker != "p2" {
		t.Errorf("Open went to %q with the picker on %q, want /projects/p2 and p2", path, picker)
	}
	h.noErrors(t)
}

func TestThePickerOpenButtonIsHiddenWhenScriptsRun(t *testing.T) {
	h := navigationHarness(t)
	var visible bool
	h.run(t, "open", h.openReady("/projects"),
		chromedp.Evaluate(`[...document.querySelectorAll('.topbar form button')].some(b => b.checkVisibility())`, &visible))
	if visible {
		t.Error("the Open button belongs to <noscript>: with scripts it must not show")
	}
}

func TestTheAttentionMarkIsOnTheFirstPaintAndFollowsAnIsolationChangeOnTheNextNavigation(t *testing.T) {
	h, cred := isolationHarness(t)
	var first bool
	h.run(t, "without script",
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(h.srv.URL+"/projects"),
		chromedp.Evaluate(isolationMark, &first),
		emulation.SetScriptExecutionDisabled(false),
	)
	if !first {
		t.Fatal("agents are blocked: the server render must carry the attention mark")
	}
	h.run(t, "open", h.openReady("/projects"))
	h.waitJS(t, "mark after hydration", isolationMark)

	cred.set.Store(true)
	h.run(t, "to the server page in place", jsClick(`.topbar a[href="/server"]`))
	h.waitURL(t, "/server")
	h.waitBar(t, "Server")
	h.waitJS(t, "mark cleared once nothing blocks agents", `!(`+isolationMark+`)`)

	cred.set.Store(false)
	h.run(t, "to the projects page in place", jsClick(`.topbar a[href="/projects"]`))
	h.waitURL(t, "/projects")
	h.waitJS(t, "mark back once agents are blocked again", isolationMark)

	cred.set.Store(true)
	h.run(t, "back", chromedp.Evaluate(`history.back()`, nil))
	h.waitURL(t, "/server")
	h.waitJS(t, "mark follows Back", `!(`+isolationMark+`)`)
	h.noErrors(t)
}

func TestTheAttentionMarkIsDroppedByAPageLoadAfterTheCauseIsFixed(t *testing.T) {
	h, cred := isolationHarness(t)
	h.run(t, "blocked", h.openReady("/projects"))
	h.waitJS(t, "marked", isolationMark)
	cred.set.Store(true)
	h.run(t, "reload", h.openReady("/projects"))
	var marked bool
	h.eval(t, isolationMark, &marked)
	if marked {
		t.Error("a fresh load must read the isolation state afresh")
	}
}

func TestBackAndForwardAcrossAnAppShellSwapKeepTheBarRight(t *testing.T) {
	h := navigationHarness(t)
	h.run(t, "open a project", h.openReady("/projects/p1"))

	h.run(t, "out of the project", jsClick(`.topbar a[href="/projects"]`))
	h.waitURL(t, "/projects")
	h.waitBar(t, "Projects")

	h.run(t, "back into the project", chromedp.Evaluate(`history.back()`, nil))
	h.waitURL(t, "/projects/p1")
	h.waitBar(t, "")
	h.run(t, "picker follows Back", railPoll(barPicker+` === 'p1'`))
	h.run(t, "bell is live after Back", railPoll(bellBadge+` === '3'`))

	h.run(t, "forward out of it", chromedp.Evaluate(`history.forward()`, nil))
	h.waitURL(t, "/projects")
	h.waitBar(t, "Projects")
	var choose string
	h.eval(t, `document.querySelector('.topbar #project-picker').selectedOptions[0].textContent.trim()`, &choose)
	if choose != "Choose a project…" {
		t.Errorf("after Forward out of the project the picker asks to choose, got %q", choose)
	}
	h.run(t, "bell is live after Forward", railPoll(bellBadge+` === '3'`))

	var bars int
	h.eval(t, `document.querySelectorAll('.topbar [data-island="top-bar"]').length`, &bars)
	if bars != 1 {
		t.Errorf("%d top bar mounts after the history walk, want 1", bars)
	}
	h.noErrors(t)
}

func TestAProjectToProjectSwapShowsTheNewProjectsPickerAndBackReturns(t *testing.T) {
	h := navigationHarness(t)
	h.run(t, "open p1", h.openReady("/projects/p1"))
	h.run(t, "pick p2", chromedp.Evaluate(`(() => { const s = document.querySelector('.topbar #project-picker'); s.value = 'p2'; s.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`, nil))
	h.waitURL(t, "/projects/p2")
	h.run(t, "picker on p2", railPoll(barPicker+` === 'p2'`))
	h.run(t, "back", chromedp.Evaluate(`history.back()`, nil))
	h.waitURL(t, "/projects/p1")
	h.run(t, "picker on p1", railPoll(barPicker+` === 'p1'`))
	h.noErrors(t)
}

func TestWithNoBundlesBuiltThePageStillLoadsWithAnEmptyMarkedMountAndItsProps(t *testing.T) {
	assets, err := newAssets(fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if assets.Built() || assets.IslandsBuilt() {
		t.Fatal("an empty bundle folder is not built")
	}
	ctx, _ := browser(t)
	deps := railWorld()
	inbox := &fakeInbox{list: inboxWorld(), stream: &sseFeed{}}
	deps.Notifications = inbox
	mux := http.NewServeMux()
	mux.Handle("/api/events", &sseFeed{})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })

	var status int
	var rendered, props, tasksButton, scripts string
	var filled bool
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Evaluate(`document.querySelector('header.topbar [data-island="top-bar"]').getAttribute('data-island-rendered') ?? ''`, &rendered),
		chromedp.Evaluate(`document.querySelector('header.topbar [data-island="top-bar"]').hasChildNodes()`, &filled),
		chromedp.Evaluate(`document.querySelector('header.topbar script[data-island-props="top-bar"]')?.textContent ?? ''`, &props),
		chromedp.Evaluate(`document.querySelector('header.topbar')?.children[0]?.tagName ?? ''`, &tasksButton),
		chromedp.Evaluate(`[...document.scripts].map(s => s.src).filter(Boolean).join(' ')`, &scripts),
		chromedp.Evaluate(`document.querySelector('main') ? 200 : 0`, &status),
	)
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Fatal("the page did not load")
	}
	if rendered != "false" || filled {
		t.Errorf("empty marked mount expected: data-island-rendered %q, content %v", rendered, filled)
	}
	if !strings.Contains(props, `"path":"/projects/p1"`) || !strings.Contains(props, `"current_project_id":"p1"`) || !strings.Contains(props, `"unread_count":3`) {
		t.Errorf("the props must still be in the page: %s", props)
	}
	if strings.Contains(scripts, "islands-") {
		t.Errorf("no client bundle exists to load, scripts: %s", scripts)
	}
}
