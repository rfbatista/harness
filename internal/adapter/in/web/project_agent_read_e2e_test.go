//go:build !windows

package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
)

// "Agents can read every repository of this project", in the browser: it
// shows on for a new project and for one stored without the setting, turning
// it off survives a reload and shows off on another person's open settings
// page through the project feed, turning it on again works, and a failed
// save shows an error and leaves the toggle as it was.
func TestProjectAgentReadSettingInTheBrowser(t *testing.T) {
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
	ctx := context.Background()
	harnessDir := t.TempDir()
	var (
		cat  catalog.Catalog
		orch *orchestration.Service
		plan *planning.Service
	)
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: harnessDir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&cat, &orch, &plan),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })
	project, err := cat.Projects.CreateProject(ctx, "harness", harnessDir)
	if err != nil {
		t.Fatal(err)
	}

	var failSaves atomic.Bool
	mux := http.NewServeMux()
	api := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan,
	}))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if failSaves.Load() && r.URL.Path == "/api/set_project_agents_read_all_repos" {
			http.Error(w, `{"error":{"code":"INTERNAL","message":"the store is read-only"}}`, http.StatusInternalServerError)
			return
		}
		api.ServeHTTP(w, r)
	})
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, ProjectSummaries: cat.Projects,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})

	const box = `#agents-read-all-repos`
	checked := func(want string) chromedp.Action {
		return waitUntil(`!document.querySelector('[data-ssr]') && document.querySelector('` + box + `').checked === ` + want)
	}
	url := srv.URL + "/projects/" + project.ID + "/settings"
	click := chromedp.Evaluate(`document.querySelector('`+box+`').click()`, nil)

	admin, other := newBrowser(t), newBrowser(t)
	admin.run("a project shows the setting on, with what it does", chromedp.Navigate(url), checked("true"),
		waitUntil(`document.querySelector('#agents-read-all-repos-hint').textContent.includes('sessions started afterwards')`))
	other.run("a second person opens the same page", chromedp.Navigate(url), checked("true"),
		chromedp.Sleep(time.Second)) // the settings page has no stream bar to wait on; let its feed connect

	admin.run("turn it off", click, waitUntil(`document.querySelector('`+box+`').getAttribute('aria-busy') !== 'true'`))
	other.run("the other page follows through the feed", checked("false"))
	admin.run("it survives a reload", chromedp.Navigate(url), checked("false"))
	if got, _ := cat.Projects.GetProject(ctx, project.ID); got.AgentsReadAllRepos {
		t.Fatal("the project still reads on")
	}

	admin.run("turn it back on", click, waitUntil(`document.querySelector('`+box+`').getAttribute('aria-busy') !== 'true'`))
	other.run("the other page follows it back on", checked("true"))

	failSaves.Store(true)
	admin.run("a failed save shows an error and leaves the toggle as it was",
		click,
		waitUntil(`document.querySelector('#agents-read-all-repos-error') !== null`),
		checked("true"),
	)
	failSaves.Store(false)
	if got, _ := cat.Projects.GetProject(ctx, project.ID); !got.AgentsReadAllRepos {
		t.Fatal("a failed save changed the project")
	}
	for _, b := range []*browser{admin, other} {
		if errs := b.jsErrors(); len(errs) > 0 {
			t.Errorf("JS errors: %v", errs)
		}
	}
}
