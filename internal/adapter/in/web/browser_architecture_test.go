package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// archRig is a browser, the pages and a fake of the architecture endpoints,
// for the Architecture page's tests.
type archRig struct {
	ctx  context.Context
	errs *errorLog
	srv  *httptest.Server
	api  *fakeArchitectureAPI
	feed *sseFeed
}

func newArchRig(t *testing.T) *archRig {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}, {ID: "p2", Name: "other"}}},
		Tasks:    fakeTickets{},
		Sessions: fakeSessions{},
		Agents:   fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{
			{ID: "r1", ProjectID: "p1", Name: "harness", RootDir: "/src/harness"},
			{ID: "r2", ProjectID: "p2", Name: "elsewhere", RootDir: "/src/elsewhere"},
		}},
		EnvFiles: fakeEnv{},
	}, assets, nil)
	feed := &sseFeed{}
	api := newFakeArchitectureAPI(feed)
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &archRig{ctx: ctx, errs: errs, srv: srv, api: api, feed: feed}
}

func (r *archRig) url(path string) string {
	return r.srv.URL + "/projects/p1/repositories/r1/architecture" + path
}

func (r *archRig) run(t *testing.T, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(r.ctx, actions...); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, r.errs.all())
	}
}

// savedRecord is a record the overview has something to count in.
func savedRecord() map[string]any {
	rec := starterRecord()
	rec["instructions"] = []any{
		map[string]any{"path": "CLAUDE.md", "kind": "rules", "applies_to": []any{"all"}, "loaded": "always"},
		map[string]any{"path": ".claude/skills/go/SKILL.md", "kind": "skill", "applies_to": []any{"go-developer"}, "loaded": "on_demand"},
	}
	rec["tools"] = []any{map[string]any{"name": "codegraph", "kind": "mcp", "used_by": []any{"all"}, "writes": false}}
	rec["sandboxes"] = []any{map[string]any{"name": "seatbelt", "runs": "Every session.", "can_reach": []any{}, "cannot_reach": []any{}, "used_by": []any{"all"}}}
	rec["orchestration"] = map[string]any{
		"routing":  []any{map[string]any{"agent": "go-developer", "model": "claude-opus-5-5"}},
		"handoffs": []any{map[string]any{"from": "software-architect", "to": "go-developer", "when": "A spec is approved."}},
	}
	rec["layers"] = []any{
		map[string]any{"name": "domain", "match": map[string]any{"folder": "internal/domain"}, "access": map[string]any{"default": "read", "agents": map[string]any{"software-architect": "change"}}},
		map[string]any{"name": "adapters", "match": map[string]any{"regex": "^internal/adapter/"}, "access": map[string]any{"default": "read", "agents": map[string]any{"software-architect": "change"}}},
	}
	rec["rules"] = []any{
		map[string]any{"id": "stay-in-your-layer", "kind": "edit_outside_layer", "mode": "blocking", "message": "Ask the architect."},
		map[string]any{"id": "archtest", "kind": "command", "at": "before_commit", "command": "go test ./...", "mode": "warning", "message": "The architecture test failed."},
	}
	return rec
}

const tabsText = `[...document.querySelectorAll('[role=tab]')].map(t => t.textContent.trim()).join('|')`
const selectedTab = `document.querySelector('[role=tab][aria-selected=true]')?.textContent.trim() ?? ''`

// The Settings → Repositories row links to the Architecture page, or to Set up
// architecture when the repository has none.
func TestRepositoryRowLinksToTheArchitecturePage(t *testing.T) {
	rig := newArchRig(t)
	var label string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.srv.URL+"/projects/p1/repositories"),
		poll(`!!document.querySelector('a[href$="/repositories/r1/architecture"]')`),
		poll(`document.querySelector('a[href$="/repositories/r1/architecture"]').textContent.trim() === 'Set up architecture'`),
		chromedp.Evaluate(`document.querySelector('a[href$="/repositories/r1/architecture"]').textContent.trim()`, &label),
	)
	if label != "Set up architecture" {
		t.Fatalf("link without a record = %q", label)
	}
	rig.api.seed(savedRecord())
	rig.run(t,
		chromedp.Navigate(rig.srv.URL+"/projects/p1/repositories"),
		poll(`document.querySelector('a[href$="/repositories/r1/architecture"]')?.textContent.trim() === 'Architecture'`),
		chromedp.Click(`a[href$="/repositories/r1/architecture"]`, chromedp.ByQuery),
	)
	waitFor(t, "the Architecture page", func() bool {
		var ok bool
		err := chromedp.Run(rig.ctx, chromedp.Evaluate(`location.pathname === '/projects/p1/repositories/r1/architecture' && !!document.querySelector('[role=tab]')`, &ok))
		return err == nil && ok // the click navigates: the old page's context is gone for a moment
	})
}

// The seven tabs, a deep link per tab, and no page reload between tabs.
func TestArchitectureTabsAndDeepLinks(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var tabs, sel, marker string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[role=tab]').length === 7`),
		chromedp.Evaluate(tabsText, &tabs),
		chromedp.Evaluate(selectedTab, &sel),
	)
	if want := "Instructions & rules|Tools|Sandboxes|Orchestration|Hooks|Layers|Output"; tabs != want {
		t.Fatalf("tabs = %q, want %q", tabs, want)
	}
	if sel != "Instructions & rules" {
		t.Fatalf("the page opens on %q", sel)
	}
	rig.run(t,
		chromedp.Evaluate(`window.__marker = 'same-page'`, nil),
		clickTab("Layers"),
		poll(`location.pathname.endsWith('/architecture/layers')`),
		chromedp.Evaluate(selectedTab, &sel),
		chromedp.Evaluate(`window.__marker`, &marker),
		poll(`[...document.querySelectorAll('[role=tabpanel]')].filter(p => p.checkVisibility()).map(p => p.id).join() === 'architecture-panel-layers'`),
	)
	if sel != "Layers" || marker != "same-page" {
		t.Fatalf("after Layers: selected %q, marker %q (a reload loses it)", sel, marker)
	}
	for url, want := range map[string]string{
		"/layers": "Layers", "/rules": "Hooks", "/file": "Output", "/diagram": "Output", "/tools": "Tools",
	} {
		rig.run(t,
			chromedp.Navigate(rig.url(url)),
			poll(`!!document.querySelector('[role=tab][aria-selected=true]')`),
			chromedp.Evaluate(selectedTab, &sel),
		)
		if sel != want {
			t.Fatalf("deep link %s selects %q, want %q", url, sel, want)
		}
	}
}

func clickTab(name string) chromedp.Action {
	return poll(fmt.Sprintf(`(() => { const t = [...document.querySelectorAll('[role=tab]')].find(t => t.textContent.trim() === %q); if (!t) return false; t.click(); return true; })()`, name))
}

// The overview: a one-line count per part and the export state badge.
func TestArchitectureOverviewCountsEachPartAndShowsTheExportState(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	rig.api.export = map[string]any{"state": "database_ahead", "path": ".harness/architecture.yaml", "exported_version": 0}
	var counts map[string]string
	var badge string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`document.querySelectorAll('[data-count]').length === 6`),
		chromedp.Evaluate(`Object.fromEntries([...document.querySelectorAll('[data-count]')].map(e => [e.dataset.count, e.textContent.trim()]))`, &counts),
		chromedp.Evaluate(`document.querySelector('[data-export-state]')?.textContent.trim() ?? ''`, &badge),
	)
	want := map[string]string{
		"instructions":  "2 files",
		"tools":         "1 tool",
		"sandboxes":     "1 sandbox",
		"orchestration": "1 route · 1 hand-off",
		"hooks":         "2 rules · 1 blocking",
		"layers":        "2 layers",
	}
	for k, v := range want {
		if counts[k] != v {
			t.Errorf("count %s = %q, want %q", k, counts[k], v)
		}
	}
	if !strings.Contains(badge, "Database ahead") {
		t.Errorf("export badge = %q", badge)
	}
}

// A repository of another project is not found, as if it did not exist.
func TestArchitectureOfAnotherProjectsRepositoryIsNotFound(t *testing.T) {
	rig := newArchRig(t)
	res, err := http.Get(rig.srv.URL + "/projects/p1/repositories/r2/architecture")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
}

// buttonText reads a button's text in a scope, or "" when there is none.
func hasButton(scope, label string) string {
	return fmt.Sprintf(`!![...document.querySelectorAll(%q + ' button')].find(b => b.textContent.trim().startsWith(%q))`, scope, label)
}

const patternField = `#architecture-pattern`
const saveBar = `[data-save-bar]`

func barVisible() string {
	return `(() => { const b = document.querySelector('[data-save-bar]'); return !!b && b.checkVisibility() })()`
}

// Editing any field shows the save bar; Discard restores the saved version and
// Save stores the draft against the version it started from.
func TestArchitectureSaveBarDiscardAndSave(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var value string
	var shown bool
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('`+patternField+`')`),
		chromedp.Evaluate(barVisible(), &shown),
	)
	if shown {
		t.Fatal("the save bar shows before any edit")
	}
	rig.run(t,
		setField(patternField, "hexagonal", "input"),
		poll(barVisible()),
		poll(`document.querySelector('[data-save-bar]').textContent.includes('Unsaved changes')`),
		clickButton(saveBar, "Discard"),
		poll(`!`+barVisible()),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &value),
	)
	if value != "" {
		t.Fatalf("after Discard the field holds %q", value)
	}
	rig.run(t,
		setField(patternField, "hexagonal", "input"),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!`+barVisible()),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &value),
	)
	if value != "hexagonal" {
		t.Fatalf("after Save the field holds %q", value)
	}
	if got := rig.api.lastSaved()["pattern"]; got != "hexagonal" {
		t.Fatalf("the server stored pattern %v", got)
	}
}

// Review diff lists what changes against the saved version.
func TestArchitectureReviewDiffListsTheChanges(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var diff string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('`+patternField+`')`),
		setField(patternField, "layered", "input"),
		poll(barVisible()),
		clickButton(saveBar, "Review diff"),
		poll(`!!document.querySelector('[data-diff]')`),
		chromedp.Evaluate(`document.querySelector('[data-diff]').textContent.replace(/\s+/g, ' ')`, &diff),
	)
	if !strings.Contains(diff, "Pattern") || !strings.Contains(diff, "layered") {
		t.Fatalf("diff = %q", diff)
	}
}

// A save on an outdated version keeps the draft and offers Reload and Review diff.
func TestArchitectureSaveOnAnOutdatedVersionKeepsTheDraft(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var value, banner string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('`+patternField+`')`),
		setField(patternField, "mine", "input"),
		poll(barVisible()),
	)
	rig.api.bumpSilently(func(rec map[string]any) { rec["pattern"] = "theirs" })
	rig.run(t,
		clickButton(saveBar, "Save"),
		poll(`!!document.querySelector('[data-conflict]')`),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &value),
		chromedp.Evaluate(`document.querySelector('[data-conflict]').textContent.replace(/\s+/g, ' ')`, &banner),
		poll(hasButton("[data-conflict]", "Reload")),
		poll(hasButton("[data-conflict]", "Review diff")),
	)
	if value != "mine" {
		t.Fatalf("the draft was lost: field holds %q", value)
	}
	if !strings.Contains(banner, "draft") {
		t.Fatalf("banner = %q", banner)
	}
	rig.run(t,
		clickButton("[data-conflict]", "Review diff"),
		poll(`document.querySelector('[data-diff]')?.textContent.includes('theirs') ?? false`),
		clickButton("[data-conflict]", "Reload"),
		poll(`document.querySelector('`+patternField+`').value === 'theirs'`),
		poll(`!document.querySelector('[data-conflict]')`),
		poll(`!`+barVisible()),
	)
}

// Each error of ARCHITECTURE_INVALID sits on its field and is counted in the bar.
func TestArchitectureSaveErrorsLandOnTheirFields(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	rig.api.refuseSaveWith = []archError{
		{"path": "/pattern", "code": "INVALID_VALUE", "message": "Unknown pattern."},
		{"path": "/layers/0/match", "code": "MISSING_FIELD", "message": "A layer needs a folder or a regex."},
	}
	var onField, bar string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('`+patternField+`')`),
		setField(patternField, "zzz", "input"),
		poll(barVisible()),
		clickButton(saveBar, "Save"),
		poll(`!!document.querySelector('[data-error-for="/pattern"]')`),
		chromedp.Evaluate(`document.querySelector('[data-error-for="/pattern"]').textContent.trim()`, &onField),
		chromedp.Evaluate(`document.querySelector('[data-save-bar]').textContent.replace(/\s+/g, ' ')`, &bar),
	)
	if onField != "Unknown pattern." {
		t.Fatalf("field error = %q", onField)
	}
	if !strings.Contains(bar, "2 problems") {
		t.Fatalf("save bar = %q", bar)
	}
	rig.run(t, poll(`document.querySelector('`+patternField+`').getAttribute('aria-invalid') === 'true'`))
}

// The page follows saves made elsewhere: a clean page reloads, a page with a
// draft shows a notice and keeps the draft.
func TestArchitectureFollowsSavesMadeElsewhere(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var value string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('`+patternField+`')`),
	)
	waitFor(t, "the page's feed", func() bool { return rig.feed.followers() >= 1 })
	rig.api.saveElsewhere(t, func(rec map[string]any) { rec["pattern"] = "from-another-tab" })
	rig.run(t, poll(`document.querySelector('`+patternField+`').value === 'from-another-tab'`))

	rig.run(t, setField(patternField, "mine", "input"), poll(barVisible()))
	rig.api.saveElsewhere(t, func(rec map[string]any) { rec["pattern"] = "again" })
	rig.run(t,
		poll(`!!document.querySelector('[data-conflict]')`),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &value),
	)
	if value != "mine" {
		t.Fatalf("the draft was replaced by %q", value)
	}
}

// The draft survives switching tabs.
func TestArchitectureDraftSurvivesSwitchingTabs(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	var value string
	rig.run(t,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.url("")),
		poll(`!!document.querySelector('`+patternField+`')`),
		setField(patternField, "kept", "input"),
		clickTab("Layers"),
		poll(`location.pathname.endsWith('/layers')`),
		clickTab("Hooks"),
		clickTab("Instructions & rules"),
		poll(`document.querySelector('`+patternField+`').value === 'kept'`),
		chromedp.Evaluate(barVisible(), nil),
		poll(barVisible()),
		chromedp.Evaluate(`document.querySelector('`+patternField+`').value`, &value),
	)
	if value != "kept" {
		t.Fatalf("draft = %q", value)
	}
}

// Every tab opens without a script error, with a record to draw. The Alpine
// CSP build rejects expressions it cannot parse (ternaries, ??) in the page
// and its tabs, which only shows as a console error.
func TestArchitectureEveryTabOpensWithoutAScriptError(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(savedRecord())
	for _, path := range []string{"", "/tools", "/sandboxes", "/orchestration", "/rules", "/layers", "/file", "/diagram"} {
		rig.run(t,
			chromedp.EmulateViewport(1280, 900),
			chromedp.Navigate(rig.url(path)),
			poll(`document.querySelectorAll('[role=tab]').length === 7`),
			chromedp.Sleep(400*time.Millisecond),
		)
		if errs := rig.errs.all(); len(errs) > 0 {
			t.Fatalf("%s: script errors: %v", path, errs)
		}
	}
}
