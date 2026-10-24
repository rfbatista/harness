//go:build !windows

package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

const isoReadAllRows = `[...document.querySelectorAll('#sandbox .project-read-all')].map(r => r.dataset.project + '=' + r.querySelector('.value').textContent.trim() + '>' + r.querySelector('a').getAttribute('href')).join(',')`

// The Agent isolation page lists every project with its "Agents can read every
// repository of this project" value and a link to where it is changed.
func TestTheAgentIsolationPageShowsEachProjectsReadAccess(t *testing.T) {
	assets := builtAssets(t)
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
	store := &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"}
	current := func() ports.SandboxDriver { return &portstest.RecordingSandboxDriver{} }
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan, Settings: cat.Settings, SandboxSettings: store, SandboxDriver: current})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects, ProjectSummaries: cat.Projects, Settings: cat.Settings, SandboxSettings: store, SandboxDriver: current}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	b := newBrowser(t)

	b.run("with no project the section says so",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`!document.querySelector('#sandbox [data-ssr]') && document.querySelector('#sandbox').innerText.includes('No projects yet.')`),
		waitUntil(`document.querySelectorAll('#sandbox .project-read-all').length === 0`),
	)

	one, err := cat.Projects.CreateProject(ctx, "alpha", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	two, err := cat.Projects.CreateProject(ctx, "beta", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := func(id string) string { return "/projects/" + id + "/settings#agents-read-all-repos" }
	b.run("each project shows on with a link to its settings",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(isoReadAllRows+` === `+quote(one.ID+"=On>"+link(one.ID)+","+two.ID+"=On>"+link(two.ID))),
	)

	b.run("turn one off on its project page",
		chromedp.Navigate(srv.URL+"/projects/"+one.ID+"/settings"),
		waitUntil(`!document.querySelector('[data-ssr]') && document.querySelector('#agents-read-all-repos').checked === true`),
		chromedp.Evaluate(`document.querySelector('#agents-read-all-repos').click()`, nil),
		waitUntil(`document.querySelector('#agents-read-all-repos').checked === false && document.querySelector('#agents-read-all-repos').getAttribute('aria-busy') !== 'true'`),
	)
	b.run("the page row shows off after a reload, the other still on",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(isoReadAllRows+` === `+quote(one.ID+"=Off>"+link(one.ID)+","+two.ID+"=On>"+link(two.ID))),
	)
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JS errors: %v", errs)
	}
}
