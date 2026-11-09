//go:build !windows

package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/commandrunner"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// servicesWorld is a real harness server with the services module: a real
// store, the real command runner and the real services API and feeds, in front
// of the real web pages.
type servicesWorld struct {
	srv     *httptest.Server
	project *domain.Project
	repo    *domain.Repository
}

func newServicesWorld(t *testing.T) *servicesWorld {
	t.Helper()
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
	root := t.TempDir()
	var cat catalog.Catalog
	var orch *orchestration.Service
	var plan *planning.Service
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: root, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&cat, &orch, &plan),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })

	db, err := sqlite.Open(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	svc := services.New(sqlite.NewServiceRepository(db), cat.Projects, cat.Projects, services.Options{
		LogDir:    t.TempDir(),
		FolderDir: t.TempDir(),
		Runners:   map[domain.ServiceKind]ports.ServiceProcessRunner{domain.ServiceKindCommand: commandrunner.Runner{}},
	})

	w := &servicesWorld{}
	if w.project, err = cat.Projects.CreateProject(ctx, "harness", root); err != nil {
		t.Fatal(err)
	}
	if w.repo, err = cat.Projects.CreateRepository(ctx, w.project.ID, "api", "", "file://"+root, root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan, Services: svc,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, ProjectSummaries: cat.Projects, Services: svc, ServiceCounter: svc,
	}, assets, nil))
	// Over HTTP/2: each page keeps an event stream open.
	w.srv = httptest.NewUnstartedServer(mux)
	w.srv.EnableHTTP2 = true
	w.srv.StartTLS()
	t.Cleanup(func() {
		w.srv.CloseClientConnections()
		w.srv.Close()
	})
	return w
}

// The Services page against a real server (spec "Web UI — Services page",
// testing decisions): the person creates a command service, starts it and sees
// it running with live log lines and the navigation count at 1, stops it, and
// sees it stopped with the count at 0.
func TestACommandServiceIsCreatedStartedAndStoppedInTheBrowser_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	const (
		row    = `document.querySelector('[data-service][data-name=ticker]')`
		status = row + `?.querySelector('[data-service-status]')?.textContent`
		nav    = `document.querySelector('nav[aria-label=Project] [data-page=services]')`
		badge  = `(() => { const b = ` + nav + `.querySelector('[data-nav-running]'); return b.checkVisibility() ? b.textContent.trim() : ''; })()`
	)
	b := newTLSBrowser(t)
	b.run("the page starts empty and the stream is live",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!document.querySelector('[data-services-empty]')`),
		waitUntil(nav+`.dataset.runningServices === '0'`),
	)
	b.run("create a command service",
		chromedp.Click(`[data-new-service]`, chromedp.ByQuery),
		waitUntil(`document.activeElement.id === 'sf-name'`),
		chromedp.SendKeys(`#sf-name`, "ticker", chromedp.ByQuery),
		chromedp.SendKeys(`#sf-line`, "echo started; while true; do echo tick; sleep 0.2; done", chromedp.ByQuery),
		chromedp.Click(`[data-form-save]`, chromedp.ByQuery),
		waitUntil(`!document.querySelector('[data-service-form]') && `+status+` === 'stopped'`),
	)
	b.run("start it: running, the count at 1",
		chromedp.Click(`[data-service][data-name=ticker] [data-action=start]`, chromedp.ByQuery),
		waitUntil(status+` === 'running'`),
		waitUntil(nav+`.dataset.runningServices === '1' && `+badge+` === '1 running'`),
	)
	b.run("its log shows the harness lines and follows the output live",
		chromedp.Click(`[data-service][data-name=ticker] [data-action=log]`, chromedp.ByQuery),
		waitUntil(`!!document.querySelector('[data-log-line][data-harness]')`),
		waitUntil(`document.querySelector('[data-log]').textContent.includes('[harness]') && document.querySelector('[data-log]').textContent.includes('started')`),
		waitUntil(`document.querySelectorAll('[data-log-line]').length > 3`),
	)
	var lines1, lines2 int
	b.run("new lines keep arriving",
		chromedp.Evaluate(`document.querySelectorAll('[data-log-line]').length`, &lines1),
		chromedp.Sleep(1200*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-log-line]').length`, &lines2),
	)
	if lines2 <= lines1 {
		t.Errorf("the log did not grow: %d lines, then %d", lines1, lines2)
	}
	b.run("stop it: stopped, the count at 0",
		chromedp.Click(`[data-service][data-name=ticker] [data-action=stop]`, chromedp.ByQuery),
		waitUntil(status+` === 'stopped'`),
		waitUntil(nav+`.dataset.runningServices === '0' && `+badge+` === ''`),
		waitUntil(`document.querySelector('[data-log]').textContent.includes('stopped')`),
	)
}

// The built-in services server in the MCP catalog (contract section 10): the
// row and the detail say Built-in and list its tools, there is no Edit, Delete
// or Duplicate, and an ordinary server beside it is unchanged.
func TestTheBuiltInServicesServerShowsInTheMcpCatalog_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"browser": {"command": "npx", "args": ["@playwright/mcp"]}}}`)

	row := func(name string) string { return `[data-mcp-row][data-key="` + name + `"]` }
	text := func(sel string) string {
		return `(document.querySelector(` + quote(sel) + `)?.innerText ?? '').replace(/\s+/g, ' ').trim()`
	}
	b := newTLSBrowser(t)
	b.run("the MCP servers tab opens",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/mcp"),
	)
	b.run("the list marks services Built-in and browser is as before",
		waitUntil(`!!document.querySelector('`+row("services")+`')`),
		waitUntil(text(row("services"))+`.includes('Built-in')`),
		waitUntil(text(row("browser"))+`.includes('stdio') && !`+text(row("browser"))+`.includes('Built-in')`),
	)
	b.run("its detail lists the tools and offers no edit",
		chromedp.Click(row("services"), chromedp.ByQuery),
		atPath(w.srv.URL+"/agents/mcp/services"),
		waitUntil(`document.querySelector('[data-mcp-detail] h2')?.textContent.trim() === 'services'`),
		waitUntil(`!!document.querySelector('[data-built-in-badge]') && document.querySelector('[data-built-in-badge]').checkVisibility()`),
		waitUntil(text(`[data-mcp-detail] [data-target]`)+`.includes('start_service') && `+text(`[data-mcp-detail] [data-target]`)+`.includes('read_service_logs')`),
		waitUntil(`document.querySelector('[data-mcp-detail] [data-actions]').checkVisibility() === false`),
	)
	b.run("an ordinary server keeps its Edit link",
		chromedp.Click(row("browser"), chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-mcp-detail] h2')?.textContent.trim() === 'browser' && document.querySelector('[data-mcp-detail] [data-actions]').checkVisibility() === true`),
	)
}

// With no server of the person's own, the MCP servers tab lists the built-in
// services server and says so, with the way to create one; once a server of
// their own exists the hint is gone.
func TestTheMcpTabSaysThereAreNoServersOfYourOwnYet_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	const rows = `[...document.querySelectorAll('[data-mcp-row]')].map(r => r.dataset.key).join(',')`
	b := newTLSBrowser(t)
	b.run("only the built-in server: its row and the hint with the link",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.srv.URL+"/agents/mcp"),
		waitUntil(rows+` === 'services'`),
		waitUntil(`document.querySelector('[data-mcp-own-empty]')?.textContent.includes('No MCP servers of your own yet.') === true`),
		waitUntil(`document.querySelector('[data-mcp-own-empty] a').getAttribute('href') === '/agents/mcp/new'`),
		waitUntil(`document.querySelector('[data-mcp-foot]').textContent.trim() === 'mcp-servers.json · 0 servers'`),
	)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"browser": {"command": "npx"}}}`)
	b.run("a server of their own takes the hint away",
		chromedp.Navigate(w.srv.URL+"/agents/mcp"),
		waitUntil(rows+` === 'browser,services'`),
		waitUntil(`!document.querySelector('[data-mcp-own-empty]')`),
	)
}
