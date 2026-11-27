package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// linksAPI answers what the pages that link to Agent system read: each repository's agent-system view, where
// r1 is missing its secret scan, and the few reads these pages make on load.
type linksAPI struct{}

func (linksAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/projects/p1/repositories/r1/agent-system":
		_, _ = w.Write([]byte(`{"effective":{"missing_required_gates":["secret_scan","lockfile"]},"values":{}}`))
	case r.URL.Path == "/api/projects/p1/repositories/r2/agent-system":
		_, _ = w.Write([]byte(`{"effective":{},"values":{}}`))
	case r.URL.Path == "/api/list_repositories":
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": []map[string]string{{"id": "r1", "name": "harness", "project_id": "p1"}, {"id": "r2", "name": "blog", "project_id": "p1"}}})
	case r.URL.Path == "/api/list_design_systems":
		_, _ = w.Write([]byte(`{"design_systems":[]}`))
	case r.URL.Path == "/api/product_owner":
		_, _ = w.Write([]byte(`{"project_id":"p1","enabled":true}`))
	default:
		http.NotFound(w, r)
	}
}

func linksServer(t *testing.T) string {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:    fakeTickets{}, Sessions: fakeSessions{}, Agents: fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{
			{ID: "r1", ProjectID: "p1", Name: "harness", RootDir: "/src/harness"},
			{ID: "r2", ProjectID: "p1", Name: "blog", RootDir: "/src/blog"},
		}},
		EnvFiles: fakeEnv{}, RunCommands: noRunCommands{},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", linksAPI{})
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestRepositoriesListCarriesAGuardLineAndALinkToAgentSystem: a repository missing required gates shows them
// in red, one that has them shows no line, and each row links to its Agent system view.
func TestRepositoriesListCarriesAGuardLineAndALinkToAgentSystem(t *testing.T) {
	url := linksServer(t)
	ctx, errs := browser(t)
	var lines, links []string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/repositories"),
		poll(`[...document.querySelectorAll('[data-guard-line]')].some(e => e.checkVisibility())`),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-guard-line]')].filter(e => e.checkVisibility()).map(e => e.closest('.row').querySelector('.weight-medium').textContent + ': ' + e.dataset.guard + ': ' + e.textContent.trim())`, &lines),
		chromedp.Evaluate(`[...document.querySelectorAll('.row a[data-agent-system-link]')].map(a => a.getAttribute('href'))`, &links),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if len(lines) != 1 || lines[0] != "harness: red: secret scan, lockfile missing" {
		t.Errorf("guard lines %q, want only the red one for harness", lines)
	}
	if strings.Join(links, " ") != "/projects/p1/agent-system?repository=r1 /projects/p1/agent-system?repository=r2" {
		t.Errorf("links %q", links)
	}
}

// TestProjectSettingsListsEachRepositoryGuardState: the Repositories and their settings section gains one row
// per repository with its Guard state and a link to its Agent system view.
func TestProjectSettingsListsEachRepositoryGuardState(t *testing.T) {
	url := linksServer(t)
	ctx, errs := browser(t)
	var rows []string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/settings"),
		poll(`document.querySelectorAll('[data-guard-line]').length === 2`),
		poll(`[...document.querySelectorAll('[data-guard-line]')].every(e => e.dataset.guard !== 'loading')`),
		chromedp.Evaluate(`[...document.querySelectorAll('#settings-repositories ~ .pane a[data-agent-system-link]')].map(a => a.getAttribute('href') + ' | ' + a.textContent.replace(/\s+/g, ' ').trim())`, &rows),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	want := []string{
		"/projects/p1/agent-system?repository=r1 | Guard of harness secret scan, lockfile missing →",
		"/projects/p1/agent-system?repository=r2 | Guard of blog Guard: every required gate has a command →",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

// TestRepositorySettingsPointsVisualQAToAgentSystem: the sentence under Run commands says a run command can be
// the UI start command for visual QA, with the link.
func TestRepositorySettingsPointsVisualQAToAgentSystem(t *testing.T) {
	url := linksServer(t)
	ctx, errs := browser(t)
	var sentence, href string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/repositories/r1/env"),
		poll(`!!document.querySelector('[data-visual-qa-note]')`),
		chromedp.Evaluate(`document.querySelector('[data-visual-qa-note]').textContent.replace(/\s+/g, ' ').trim()`, &sentence),
		chromedp.Evaluate(`document.querySelector('[data-visual-qa-note] a').getAttribute('href')`, &href),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if sentence != "A run command can be the UI start command for visual QA: declare it in Agent system ↗." {
		t.Errorf("sentence %q", sentence)
	}
	if href != "/projects/p1/agent-system?repository=r1" {
		t.Errorf("href %q", href)
	}
}

// TestArchitecturePageLinksToAgentSystem: the tab row ends with a link, not a tab, that opens Agent system with
// this repository, and the Hooks tab's empty state says where gates and protected paths live.
func TestArchitecturePageLinksToAgentSystem(t *testing.T) {
	rig := newArchRig(t)
	rig.api.seed(recordWithoutRules())
	var href, text string
	var insideTablist bool
	var lastInRow string
	var emptyText string
	rig.run(t,
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(rig.url("/rules")),
		hooksOpen(),
		poll(`!!document.querySelector('[data-agent-system-link]')`),
		chromedp.Evaluate(`document.querySelector('[data-agent-system-link]').getAttribute('href')`, &href),
		chromedp.Evaluate(`document.querySelector('[data-agent-system-link]').textContent.trim()`, &text),
		chromedp.Evaluate(`!!document.querySelector('[data-agent-system-link]').closest('[role=tablist]')`, &insideTablist),
		chromedp.Evaluate(`document.querySelector('.arch-tabrow').lastElementChild.hasAttribute('data-agent-system-link') ? 'link' : 'other'`, &lastInRow),
		poll(`!!document.querySelector('#architecture-panel-hooks [data-no-rules]')?.checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('#architecture-panel-hooks [data-no-rules]').textContent.replace(/\s+/g, ' ')`, &emptyText),
	)
	if href != "/projects/p1/agent-system?repository=r1" || text != "Agent system ↗" {
		t.Errorf("link %q %q", href, text)
	}
	if insideTablist || lastInRow != "link" {
		t.Errorf("the link is inside the tablist (%v) or not last in the row (%q)", insideTablist, lastInRow)
	}
	if !strings.Contains(emptyText, "Gates and protected paths live in Agent system") {
		t.Errorf("empty state %q", emptyText)
	}
}
