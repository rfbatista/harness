package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
)

// W1 for the Agents page's shell (spec "Web UI — the Agents page"): the top
// bar link, the routes and their tabs, the tablist's roles and keys, and the
// header's New button. None of it needs the catalog, so the page renders with
// no CatalogReader and an API that has no catalog: the browser's own read
// fails and the page says so. The catalog's content is
// TestAgentsPageShowsTheCatalogInTheBrowser, against a real server.

// agentsAPI answers nothing a catalog page needs; the page must cope.
var agentsAPI = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

func newAgentsPageServer(t *testing.T) string {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "harness", RootDir: "/src/harness"}}},
		Tasks:    fakeTickets{}, Sessions: fakeSessions{}, Agents: fakeAgents{},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", agentsAPI)
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

const alpineReady = `document.querySelector('main[x-data="agentsPage"]')?._x_dataStack !== undefined`

const agentsSelectedTab = `document.querySelector('[role=tab][aria-selected=true]')?.dataset.tab ?? ''`

// newButton reads the header's New button: its label and whether it shows.
const newButton = `(() => { const a = document.querySelector('[data-new]'); return a.checkVisibility() ? a.textContent.trim() + ' ' + a.getAttribute('href') : ''; })()`

func TestAgentsPageShellInTheBrowser(t *testing.T) {
	ctx, errs := browser(t)
	base := newAgentsPageServer(t)

	// The top bar says what Agents are and leads to the page.
	var link, title string
	var got string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(base+"/projects/new"),
		chromedp.WaitVisible(`a[href="/agents"]`, chromedp.ByQuery),
		chromedp.Text(`header a[href="/agents"]`, &link, chromedp.ByQuery),
		chromedp.AttributeValue(`header a[href="/agents"]`, "title", &title, nil, chromedp.ByQuery),
		chromedp.Click(`header a[href="/agents"]`, chromedp.ByQuery),
		waitForPath(base+"/agents", &got),
		chromedp.WaitVisible(`[role=tablist]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("top bar link: %v\nJS errors: %v", err, errs.all())
	}
	if strings.TrimSpace(link) != "Agents" || title != "Agents are shared by every project" {
		t.Errorf("top bar link = %q (title %q)", link, title)
	}

	// Each route and deep link selects its tab.
	for path, want := range map[string]string{
		"/agents": "agents", "/agents/skills": "skills", "/agents/bundles": "bundles", "/agents/mcp": "mcp", "/agents/tools": "tools",
		"/agents/skills/tdd": "skills", "/agents/ux-designer": "agents", "/agents/bundles/ux": "bundles", "/agents/mcp/github": "mcp",
		"/agents/skills/new": "skills", "/agents/ux-designer/edit": "agents",
	} {
		var tab string
		var tabs, panels int
		var visiblePanels []string
		err := chromedp.Run(ctx,
			chromedp.Navigate(base+path),
			chromedp.Poll(alpineReady, nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(agentsSelectedTab, &tab),
			chromedp.Evaluate(`document.querySelectorAll('[role=tablist] [role=tab]').length`, &tabs),
			chromedp.Evaluate(`document.querySelectorAll('[role=tabpanel]').length`, &panels),
			chromedp.Evaluate(`[...document.querySelectorAll('[role=tabpanel]')].filter(p => p.checkVisibility()).map(p => p.getAttribute('aria-labelledby'))`, &visiblePanels),
		)
		if err != nil {
			t.Fatalf("%s: %v\nJS errors: %v", path, err, errs.all())
		}
		if tab != want || tabs != 5 || panels != 5 || len(visiblePanels) != 1 || visiblePanels[0] != "agents-tab-"+want {
			t.Errorf("%s: selected %q (want %q), %d tabs, %d panels, visible panels %v", path, tab, want, tabs, panels, visiblePanels)
		}
	}

	// The deep link names its item.
	var note string
	err = chromedp.Run(ctx,
		chromedp.Navigate(base+"/agents/skills/tdd"),
		chromedp.Poll(alpineReady, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=tabpanel]:not([hidden]) [data-item]').textContent.trim()`, &note),
	)
	if err != nil || note != "tdd" {
		t.Errorf("deep link item = %q (%v)", note, err)
	}

	// Arrow keys, Home and End move between the tabs; the address follows.
	keys := []struct {
		key  string
		tab  string
		path string
	}{
		{kb.ArrowRight, "skills", "/agents/skills"},
		{kb.ArrowRight, "bundles", "/agents/bundles"},
		{kb.End, "tools", "/agents/tools"},
		{kb.ArrowRight, "agents", "/agents"},
		{kb.ArrowLeft, "tools", "/agents/tools"},
		{kb.Home, "agents", "/agents"},
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/agents"),
		chromedp.Poll(alpineReady, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Focus(`#agents-tab-agents`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("focus the tablist: %v", err)
	}
	for _, k := range keys {
		var tab, path, focused string
		var tabindexes []string
		err := chromedp.Run(ctx,
			chromedp.KeyEvent(k.key),
			chromedp.Poll(agentsSelectedTab+` === `+quote(k.tab), nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(`location.pathname`, &path),
			chromedp.Evaluate(`document.activeElement?.dataset?.tab ?? ''`, &focused),
			chromedp.Evaluate(`[...document.querySelectorAll('[role=tab]')].filter(t => t.tabIndex === 0).map(t => t.dataset.tab)`, &tabindexes),
			chromedp.Evaluate(agentsSelectedTab, &tab),
		)
		if err != nil {
			t.Fatalf("key %q: %v\nJS errors: %v", k.key, err, errs.all())
		}
		if path != k.path || focused != k.tab || len(tabindexes) != 1 || tabindexes[0] != k.tab {
			t.Errorf("key %q: at %q, focus on %q, tabbable %v; want %q on %q", k.key, path, focused, tabindexes, k.path, k.tab)
		}
	}

	// The New button follows the tab; Tools has none.
	for _, c := range []struct{ tab, want string }{
		{"agents", "New agent /agents/new"},
		{"skills", "New skill /agents/skills/new"},
		{"bundles", "New bundle /agents/bundles/new"},
		{"mcp", "New MCP server /agents/mcp/new"},
		{"tools", ""},
	} {
		var button string
		err := chromedp.Run(ctx,
			chromedp.Click(`#agents-tab-`+c.tab, chromedp.ByQuery),
			chromedp.Poll(agentsSelectedTab+` === `+quote(c.tab), nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(newButton, &button),
		)
		if err != nil {
			t.Fatalf("tab %s: %v", c.tab, err)
		}
		if button != c.want {
			t.Errorf("tab %s: New button = %q, want %q", c.tab, button, c.want)
		}
	}

	// Going back returns to the earlier tab.
	var back string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Poll(agentsSelectedTab+` === 'mcp'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(agentsSelectedTab, &back),
	); err != nil {
		t.Errorf("back: %v", err)
	}

	// The catalog could not be read: the page says so and still works.
	var banner string
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.querySelector('.banner[role=alert]') !== null`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.banner[role=alert]').textContent.replace(/\s+/g, ' ').trim()`, &banner),
	); err != nil {
		t.Errorf("no banner for the failed catalog read: %v", err)
	}
	if !strings.Contains(banner, "Check again") {
		t.Errorf("banner = %q", banner)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
