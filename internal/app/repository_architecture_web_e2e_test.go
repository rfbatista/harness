//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
)

// The Architecture page against the server as it runs: the real record,
// preview, save, version check, export and project feed, on a real git
// checkout, with a headless Chrome as the person. The page's own behaviour is
// covered in detail against a fake in internal/adapter/in/web; this is the one
// flow that proves the two sides agree.
func TestRepositoryArchitectureInTheBrowserAgainstTheRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	dir := t.TempDir()
	checkout := filepath.Join(dir, "widgets")
	gitInit(t, checkout)
	if err := os.WriteFile(filepath.Join(checkout, "CLAUDE.md"), []byte("# Rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(checkout, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "internal", "core.go"), []byte("package internal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "CLAUDE.md", "internal/core.go"}, {"-c", "user.email=e2e@test", "-c", "user.name=e2e", "commit", "-q", "-m", "rules"}} {
		if out, err := exec.Command("git", append([]string{"-C", checkout}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	var (
		cat  catalog.Catalog
		orch *orchestration.Service
		plan *planning.Service
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&cat, &orch, &plan),
		fx.NopLogger,
	)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	for _, name := range []string{"software-architect", "go-developer", "devops"} {
		if _, err := cat.Agents.CreateAgent(ctx, name, "", "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	p, err := cat.Projects.CreateProject(ctx, "widgets", checkout)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := cat.Projects.CreateRepository(ctx, p.ID, "widgets", "", "file://"+checkout, checkout)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, Agents: cat.Agents, Architecture: cat.Architecture,
		RepositoryArchitectures: cat.RepositoryArchitectures, Sessions: orch, Planning: plan,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects,
	}, assets, nil))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true // the page holds an event stream open
	srv.StartTLS()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	page := srv.URL + "/projects/" + p.ID + "/repositories/" + repo.ID + "/architecture"
	post := func(verb string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		res, err := srv.Client().Post(srv.URL+"/api/"+verb, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}

	me := newPerson(t)
	poll := func(js string) chromedp.Action {
		return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(10_000_000_000))
	}
	click := func(sel string) chromedp.Action {
		return poll(`(() => { const e = document.querySelector(` + jsString(sel) + `); if (!e || e.disabled) return false; e.click(); return true; })()`)
	}
	const saveButton = `[data-save-bar] button[data-variant=primary]`

	// First setup: the empty state, what the harness found, one layer, save.
	me.run(t, "open the empty page",
		chromedp.Navigate(page),
		poll(`!!document.querySelector('#architecture-empty [data-action=discover]')`),
		click(`#architecture-empty [data-action=discover]`),
		poll(`document.querySelectorAll('[data-candidates] input[type=checkbox]').length > 0`),
		click(`#architecture-empty [data-action=confirm]`),
		poll(`!!document.querySelector('[data-setup-done]')`),
		click(saveButton),
		poll(`!document.querySelector('#architecture-empty')`),
	)
	me.run(t, "add a layer",
		chromedp.Navigate(page+"/layers"),
		click(`#architecture-panel-layers [data-add-layer=folder]`),
		poll(`!!document.querySelector('#architecture-panel-layers [data-layer="0"] [data-field=pattern]')`),
		chromedp.SetValue(`#architecture-panel-layers [data-layer="0"] [data-field=name]`, "core", chromedp.ByQuery),
		chromedp.SetValue(`#architecture-panel-layers [data-layer="0"] [data-field=pattern]`, "internal", chromedp.ByQuery),
		// the live preview, answered by the real server for the unsaved draft
		poll(`document.querySelector('#architecture-panel-layers [data-layer="0"] [data-layer-count]')?.textContent.includes('1 file')`),
		poll(`!!document.querySelector('[data-save-bar]')?.checkVisibility()`),
		click(saveButton),
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	// An invalid draft: the server's details.errors land on the page, nothing is stored.
	me.run(t, "an invalid save",
		click(`#architecture-panel-layers [data-add-layer=folder]`),
		poll(`!!document.querySelector('#architecture-panel-layers [data-layer="1"]')`),
		click(saveButton),
	)
	me.run(t, "the problems show",
		poll(`!!document.querySelector('[data-problems]')`),
	)
	me.run(t, "the bar counts them",
		poll(`document.querySelector('[data-save-bar]').textContent.includes('problem')`),
		click(`[data-save-bar] button[data-variant=ghost]`), // Discard
		poll(`!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	status, got := post("get_repository_architecture", map[string]any{"repository_id": repo.ID})
	if status != 200 || got["version"] != float64(2) {
		t.Fatalf("after two saves: %d %v", status, got)
	}
	layers, _ := got["record"].(map[string]any)["layers"].([]any)
	if len(layers) != 1 {
		t.Fatalf("saved layers = %v", layers)
	}

	// A save made elsewhere while the person has a draft: a notice, and the draft stays.
	me.run(t, "edit without saving",
		chromedp.Navigate(page),
		poll(`!!document.querySelector('#architecture-pattern')`),
		chromedp.SetValue(`#architecture-pattern`, "mine", chromedp.ByQuery),
		poll(`!!document.querySelector('[data-save-bar]')?.checkVisibility()`),
	)
	record := got["record"].(map[string]any)
	record["pattern"] = "theirs"
	if status, out := post("save_repository_architecture", map[string]any{"repository_id": repo.ID, "base_version": 2, "record": record}); status != 200 {
		t.Fatalf("save elsewhere: %d %v", status, out)
	}
	var draft string
	me.run(t, "see the notice",
		poll(`!!document.querySelector('[data-conflict]')`),
		chromedp.Evaluate(`document.querySelector('#architecture-pattern').value`, &draft),
		click(`[data-conflict] button`), // Reload
		poll(`document.querySelector('#architecture-pattern').value === 'theirs'`),
	)
	if draft != "mine" {
		t.Fatalf("the draft was replaced by %q", draft)
	}

	// Export commits the file; the state becomes in sync.
	me.run(t, "export",
		chromedp.Navigate(page+"/file"),
		poll(`!!document.querySelector('[data-export]') && !document.querySelector('[data-export]').disabled`),
		click(`[data-export]`),
		poll(`!!document.querySelector('[data-export-result]')`),
		poll(`document.querySelector('[data-file-state-title]')?.textContent.includes('In sync')`),
	)
	out, err := exec.Command("git", "-C", checkout, "log", "-1", "--format=%s").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "chore(architecture): export repository architecture v3") {
		t.Fatalf("the export commit: %q (%v)", out, err)
	}

	// The diagram opens from its deep link.
	me.run(t, "diagram",
		chromedp.Navigate(page+"/diagram"),
		poll(`!!document.querySelector('[data-diagram-picture] svg g[data-layer]')`),
	)
}

// A rule that fails in a session shows on the overview as it happens, against
// the real hooks: an agent's edit outside its layer is blocked, and the page,
// already open, lists the event.
func TestRepositoryArchitectureRuleEventsShowLiveAgainstTheRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	h := newArchHooks(t, "blocking")
	api, _ := url.Parse(h.srv.URL)
	proxy := httputil.NewSingleHostReverseProxy(api)
	proxy.FlushInterval = -1 // the project feed is a stream
	mux := http.NewServeMux()
	mux.Handle("/api/", proxy)
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: h.cat.Projects, Tasks: h.plan, Sessions: h.orch, Agents: h.cat.Agents, Repositories: h.cat.Projects, EnvFiles: h.cat.Projects,
	}, assets, nil))
	front := httptest.NewUnstartedServer(mux)
	front.EnableHTTP2 = true
	front.StartTLS()
	t.Cleanup(func() {
		front.CloseClientConnections()
		front.Close()
	})

	me := newPerson(t)
	me.run(t, "open the overview",
		chromedp.Navigate(front.URL+"/projects/"+h.project+"/repositories/"+h.repo+"/architecture"),
		chromedp.Poll(`document.querySelectorAll('[role=tab]').length === 7`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	)
	time.Sleep(time.Second) // the page's feed follow is open

	a := h.as("go-developer")
	runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", filepath.Join(a.worktree, "internal", "domain", "zone.go")))
	var row string
	me.run(t, "the event appears",
		chromedp.Poll(`document.querySelectorAll('[data-rule-event]').length === 1`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-rule-event]').textContent.replace(/\s+/g, ' ')`, &row),
	)
	for _, want := range []string{"go-developer", "stay-in-your-layer", "Blocked", "internal/domain/zone.go", "domain"} {
		if !strings.Contains(row, want) {
			t.Errorf("event row %q lacks %q", row, want)
		}
	}
}
