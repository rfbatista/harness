package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// agentSystemAPI is the agent-system API of one project in memory: what the project and one repository set,
// and one repository missing a required gate. It answers the shapes of the contract.
type agentSystemAPI struct {
	mu       sync.Mutex
	project  map[string]any
	repo     map[string]map[string]any
	patches  []string
	repoList []map[string]string
}

func newAgentSystemAPI(repos ...string) *agentSystemAPI {
	a := &agentSystemAPI{project: map[string]any{}, repo: map[string]map[string]any{}}
	for _, r := range repos {
		a.repoList = append(a.repoList, map[string]string{"id": r, "name": r, "project_id": "p1"})
		a.repo[r] = map[string]any{}
	}
	return a
}

func (a *agentSystemAPI) field(repo, path string, def any) map[string]any {
	if repo != "" {
		if v, ok := a.repo[repo][path]; ok {
			return map[string]any{"value": v, "source": "repository"}
		}
	}
	if v, ok := a.project[path]; ok {
		return map[string]any{"value": v, "source": "project"}
	}
	return map[string]any{"value": def, "source": "harness"}
}

func (a *agentSystemAPI) view(repo string) map[string]any {
	eff := map[string]any{
		"learn": map[string]any{
			"context_map": map[string]any{"enabled": a.field(repo, "learn.context_map.enabled", true)},
			"task_memory": map[string]any{"require_update": a.field(repo, "learn.task_memory.require_update", true)},
		},
		"score": map[string]any{"trajectory_recording": map[string]any{"default": a.field(repo, "score.trajectory_recording.default", false)}},
	}
	values := a.project
	if repo != "" {
		values = a.repo[repo]
		if repo == a.repoList[0]["id"] {
			eff["missing_required_gates"] = []string{"secret_scan"}
		}
	}
	return map[string]any{"effective": eff, "values": values}
}

func (a *agentSystemAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/list_repositories" {
		json.NewEncoder(w).Encode(map[string]any{"repositories": a.repoList})
		return
	}
	path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/projects/p1"), "/agent-system")
	repo := strings.TrimPrefix(path, "/repositories/")
	if path == "" {
		repo = ""
	}
	if r.Method == http.MethodPatch {
		var patch map[string]any
		json.NewDecoder(r.Body).Decode(&patch)
		a.patches = append(a.patches, r.URL.Path+" "+toJSON(patch))
		target := a.project
		if repo != "" {
			target = a.repo[repo]
		}
		for k, v := range patch {
			if v == nil {
				delete(target, k)
			} else {
				target[k] = v
			}
		}
	}
	json.NewEncoder(w).Encode(a.view(repo))
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func (a *agentSystemAPI) patchLog() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.patches...)
}

func agentSystemServer(t *testing.T, api *agentSystemAPI) *httptest.Server {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	w := board()
	mux := http.NewServeMux()
	mux.Handle("/api/list_repositories", api)
	mux.Handle("/api/projects/p1/", api)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestAgentSystemPageInTheBrowser: the page shows the project defaults as ledger rows with their sources,
// a toggle saves at once as a PATCH of that path, the scope switch moves to a repository (and the address
// follows), where an inherited row offers Override and an override offers Reset, and a repository missing a
// required gate carries the red dot.
func TestAgentSystemPageInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)

	row := func(path string) string { return `document.querySelector('.ledger-row[data-path="` + path + `"]')` }
	var layers, source, valueAfter, scopeAddress, action, replaced, dot string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.as-layer > header h2')].map(h => h.textContent).join()`, &layers),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.src').textContent`, &source),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('[role=switch]').click()`, nil),
		chromedp.Poll(`(`+row("learn.context_map.enabled")+`.querySelector('.src').textContent || '').includes('project')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('[role=switch]').getAttribute('aria-checked')`, &valueAfter),

		chromedp.Evaluate(`document.querySelector('.toolbar [aria-label=Scope] button:last-child').click()`, nil),
		chromedp.Poll(`!!document.querySelector('select[aria-label=Repository]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`!!document.querySelector('.toolbar .status[data-state=missing]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`new URL(location.href).searchParams.get('repository')`, &scopeAddress),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.act').textContent`, &action),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.act button').click()`, nil),
		chromedp.Poll(`(`+row("learn.context_map.enabled")+`.querySelector('.act').textContent || '') === 'Reset'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.src').textContent`, &replaced),
		chromedp.Evaluate(`document.querySelector('.toolbar .status[data-state=missing]')?.getAttribute('aria-label') ?? ''`, &dot),
		chromedp.FullScreenshot(&shot, 90),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if layers != "Learn,Score" {
		t.Errorf("layers %q", layers)
	}
	if !strings.Contains(source, "harness default") {
		t.Errorf("source of an untouched row %q", source)
	}
	if valueAfter != "false" {
		t.Errorf("the toggle reads %q after saving, want false", valueAfter)
	}
	if scopeAddress != "harness" {
		t.Errorf("?repository= is %q, want harness (the first repository)", scopeAddress)
	}
	if action != "Override" {
		t.Errorf("an inherited row's action is %q, want Override", action)
	}
	if !strings.Contains(replaced, "override") || !strings.Contains(replaced, "project: off") {
		t.Errorf("an override reads %q, want it to name the project value it replaces", replaced)
	}
	if dot != "missing a required gate" {
		t.Errorf("red dot %q", dot)
	}
	patches := api.patchLog()
	want := []string{
		`/api/projects/p1/agent-system {"learn.context_map.enabled":false}`,
		`/api/projects/p1/repositories/harness/agent-system {"learn.context_map.enabled":false}`,
	}
	if len(patches) != 2 || patches[0] != want[0] || patches[1] != want[1] {
		t.Errorf("patches %q, want %q", patches, want)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/agent-system.jpg", shot, 0o644)
	}
}

// TestAgentSystemPageOpensTheRepositoryTheAddressNames: ?repository= picks the scope on load, and Back
// returns to the project defaults.
func TestAgentSystemPageOpensTheRepositoryTheAddressNames(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var selected, afterBack string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=blog"),
		chromedp.Poll(`!!document.querySelector('select[aria-label=Repository]') && !!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('select[aria-label=Repository]').value`, &selected),
		chromedp.Evaluate(`document.querySelector('.toolbar [aria-label=Scope] button:first-child').click()`, nil),
		chromedp.Poll(`!document.querySelector('select[aria-label=Repository]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Poll(`!!document.querySelector('select[aria-label=Repository]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('select[aria-label=Repository]').value`, &afterBack),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if selected != "blog" || afterBack != "blog" {
		t.Errorf("selected %q, after Back %q, want blog both times", selected, afterBack)
	}
}
