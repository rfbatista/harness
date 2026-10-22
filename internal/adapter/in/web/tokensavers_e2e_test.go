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
	"operators-mcp/internal/domain"
)

// tokenSaverWorld is the server as it runs (catalog, planning, orchestration,
// the HTTP API and the pages, over an in-memory database) on one test
// server, with a project that has one repository and one task.
type tokenSaverWorld struct {
	t       *testing.T
	ctx     context.Context
	cat     catalog.Catalog
	plan    *planning.Service
	srv     *httptest.Server
	root    string
	project *domain.Project
	repo    *domain.Repository
	task    *domain.Ticket
}

func newTokenSaverWorld(t *testing.T) *tokenSaverWorld {
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
	w := &tokenSaverWorld{t: t, ctx: context.Background(), root: t.TempDir()}
	var orch *orchestration.Service
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: w.root, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&w.cat, &orch, &w.plan),
		fx.NopLogger,
	)
	if err := fxApp.Start(w.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })

	if w.project, err = w.cat.Projects.CreateProject(w.ctx, "harness", w.root); err != nil {
		t.Fatal(err)
	}
	w.repo = w.addRepository()
	w.task = w.addTask()

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: w.cat.Projects, ProjectFeed: w.cat.Projects, Agents: w.cat.Agents, Sessions: orch, Planning: w.plan,
		TokenSavers: w.cat.Capabilities,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: w.cat.Projects, Tasks: w.plan, Sessions: orch, Agents: w.cat.Agents, Repositories: w.cat.Projects,
		EnvFiles: w.cat.Projects, ProjectSummaries: w.cat.Projects, TokenSavers: w.cat.Capabilities,
	}, assets, nil))
	// Over HTTP/2: each page keeps an event stream open, and HTTP/1.1's six
	// connections per host run out across the navigations.
	w.srv = httptest.NewUnstartedServer(mux)
	w.srv.EnableHTTP2 = true
	w.srv.StartTLS()
	t.Cleanup(func() {
		w.srv.CloseClientConnections() // the pages' event streams never end on their own
		w.srv.Close()
	})
	return w
}

func (w *tokenSaverWorld) addRepository() *domain.Repository {
	w.t.Helper()
	r, err := w.cat.Projects.CreateRepository(w.ctx, w.project.ID, "api", "", "file://"+w.root, w.root)
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

func (w *tokenSaverWorld) addTask() *domain.Ticket {
	w.t.Helper()
	task, err := w.plan.CreateTicket(w.ctx, w.project.ID, "Cut tokens", "", domain.TicketStatusInProgress, "")
	if err != nil {
		w.t.Fatal(err)
	}
	return task
}

func (w *tokenSaverWorld) settingsURL() string {
	return w.srv.URL + "/projects/" + w.project.ID + "/settings"
}
func (w *tokenSaverWorld) repositoryURL() string {
	return w.srv.URL + "/projects/" + w.project.ID + "/repositories/" + w.repo.ID + "/env"
}
func (w *tokenSaverWorld) taskURL() string {
	return w.srv.URL + "/projects/" + w.project.ID + "/tasks/" + w.task.ID
}

// resolved is what the server resolves saver to for a session of
// (project, repository, task), as "on (project)".
func (w *tokenSaverWorld) resolved(saver domain.TokenSaver, projectID, repositoryID, taskID string) string {
	w.t.Helper()
	all, err := w.cat.Capabilities.ResolveTokenSavers(w.ctx, projectID, repositoryID, taskID)
	if err != nil {
		w.t.Fatal(err)
	}
	for _, r := range all {
		if r.Saver == saver {
			return string(r.Value) + " (" + string(r.Level) + ")"
		}
	}
	w.t.Fatalf("%s not resolved", saver)
	return ""
}

// The Token savers control of the page, once its live rows replaced the
// server's first paint.
const activationsSection = `document.querySelector('section[aria-labelledby=token-savers-title]')`

func saverRow(saver string) string {
	return activationsSection + `?.querySelector('[data-saver="` + saver + `"]:not([data-ssr] *)')`
}

// tokenSaversReady waits for the control's live rows (the strip's panel
// holding it on the task page is opened first).
func tokenSaversReady() chromedp.Action {
	return chromedp.Tasks{
		chromedp.Evaluate(`(() => { const b = document.querySelector('[data-band="token-savers"]'); if (b && b.getAttribute('aria-expanded') !== 'true') b.click() })()`, nil),
		waitUntil(activationsSection + ` && !` + activationsSection + `.querySelector('[data-ssr]') && !!` + saverRow("rtk")),
	}
}

// summaryIs waits until saver's line reads want.
func summaryIs(saver, want string) chromedp.Action {
	return waitUntil(`(` + saverRow(saver) + `?.querySelector('[data-resolved]')?.textContent.trim() ?? '') === ` + quote(want))
}

// choose sets saver's select to value as a person would.
func choose(saver, value string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const s = `+saverRow(saver)+`.querySelector('select');
		s.value = `+quote(value)+`; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
}

// selected waits until saver's select shows value.
func selected(saver, value string) chromedp.Action {
	return waitUntil(`(` + saverRow(saver) + `?.querySelector('select')?.value ?? '') === ` + quote(value))
}

// TestTokenSaverActivationsInTheWebUI_EndToEnd sets rtk at each level on its
// page: on for the project, which the repository inherits; off for the
// repository, on for the task. Each page shows the saver's own value, what
// it resolves to and from which level, and keeps them across a reload.
func TestTokenSaverActivationsInTheWebUI_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	b := newTLSBrowser(t)

	b.run("the project's Settings page starts every saver off",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.settingsURL()),
		tokenSaversReady(),
		selected("rtk", "off"),
		summaryIs("rtk", "off — resolves off (project)"),
		summaryIs("caveman", "off — resolves off (project)"),
		summaryIs("codegraph", "off — resolves off (project)"),
	)
	b.run("turn rtk on for the project",
		choose("rtk", "on"),
		summaryIs("rtk", "on — resolves on (project)"),
		bothSchemes("token-savers-project"),
	)

	b.run("the repository inherits it",
		chromedp.Navigate(w.repositoryURL()),
		tokenSaversReady(),
		selected("rtk", "inherit"),
		summaryIs("rtk", "inherit — resolves on (project)"),
		summaryIs("caveman", "inherit — resolves off (project)"),
	)
	b.run("turn rtk off for the repository",
		choose("rtk", "off"),
		summaryIs("rtk", "off — resolves off (repository)"),
		bothSchemes("token-savers-repository"),
	)

	b.run("the task inherits the repository's",
		chromedp.Navigate(w.taskURL()),
		tokenSaversReady(),
		selected("rtk", "inherit"),
		summaryIs("rtk", "inherit — resolves off (repository)"),
	)
	b.run("turn rtk on for the task",
		choose("rtk", "on"),
		summaryIs("rtk", "on — resolves on (task)"),
		bothSchemes("token-savers-task"),
	)

	b.run("after a reload, the task keeps it",
		chromedp.Reload(),
		tokenSaversReady(),
		selected("rtk", "on"),
		summaryIs("rtk", "on — resolves on (task)"),
	)
	b.run("so does the repository",
		chromedp.Navigate(w.repositoryURL()),
		tokenSaversReady(),
		selected("rtk", "off"),
		summaryIs("rtk", "off — resolves off (repository)"),
	)
	b.run("and the project",
		chromedp.Navigate(w.settingsURL()),
		tokenSaversReady(),
		selected("rtk", "on"),
		summaryIs("rtk", "on — resolves on (project)"),
		summaryIs("caveman", "off — resolves off (project)"),
	)
	if got := w.resolved(domain.TokenSaverRTK, w.project.ID, w.repo.ID, w.task.ID); got != "on (task)" {
		t.Errorf("a session of the task resolves rtk %s, want on (task)", got)
	}
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JS errors: %v", errs)
	}
}

// TestTokenSaverActivationsGoWithTheirOwner_EndToEnd deletes a task, then a
// repository, then a project that each set rtk: their activations go with
// them, and an owner recreated in their place starts at the default.
func TestTokenSaverActivationsGoWithTheirOwner_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	b := newTLSBrowser(t)

	b.run("rtk on for the project, off for the repository, on for the task",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(w.settingsURL()),
		tokenSaversReady(),
		choose("rtk", "on"),
		summaryIs("rtk", "on — resolves on (project)"),
		chromedp.Navigate(w.repositoryURL()),
		tokenSaversReady(),
		choose("rtk", "off"),
		summaryIs("rtk", "off — resolves off (repository)"),
		chromedp.Navigate(w.taskURL()),
		tokenSaversReady(),
		choose("rtk", "on"),
		summaryIs("rtk", "on — resolves on (task)"),
	)

	project, repo, task := w.project, w.repo, w.task
	if err := w.plan.DeleteTicket(w.ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.resolved(domain.TokenSaverRTK, project.ID, repo.ID, task.ID); got != "off (repository)" {
		t.Errorf("after the task's delete, its id resolves rtk %s, want off (repository)", got)
	}
	w.task = w.addTask()
	b.run("a new task starts inheriting",
		chromedp.Navigate(w.taskURL()),
		tokenSaversReady(),
		selected("rtk", "inherit"),
		summaryIs("rtk", "inherit — resolves off (repository)"),
	)

	if err := w.cat.Projects.DeleteRepository(w.ctx, repo.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.resolved(domain.TokenSaverRTK, project.ID, repo.ID, ""); got != "on (project)" {
		t.Errorf("after the repository's delete, its id resolves rtk %s, want on (project)", got)
	}
	w.repo = w.addRepository()
	b.run("the repository added again starts inheriting",
		chromedp.Navigate(w.repositoryURL()),
		tokenSaversReady(),
		selected("rtk", "inherit"),
		summaryIs("rtk", "inherit — resolves on (project)"),
	)

	b.run("rtk on for the new task, off for the repository added again",
		chromedp.Navigate(w.taskURL()),
		tokenSaversReady(),
		choose("rtk", "on"),
		summaryIs("rtk", "on — resolves on (task)"),
		chromedp.Navigate(w.repositoryURL()),
		tokenSaversReady(),
		choose("rtk", "off"),
		summaryIs("rtk", "off — resolves off (repository)"),
	)
	if err := w.cat.Projects.DeleteProject(w.ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.resolved(domain.TokenSaverRTK, project.ID, "", ""); got != "off (project)" {
		t.Errorf("after the project's delete, its id resolves rtk %s, want off (project)", got)
	}
	if got := w.resolved(domain.TokenSaverRTK, project.ID, w.repo.ID, w.task.ID); got != "off (project)" {
		t.Errorf("after the project's delete, its repository and task resolve rtk %s, want off (project)", got)
	}
	var err error
	if w.project, err = w.cat.Projects.CreateProject(w.ctx, "harness", w.root); err != nil {
		t.Fatal(err)
	}
	b.run("the project created again starts off",
		chromedp.Navigate(w.settingsURL()),
		tokenSaversReady(),
		selected("rtk", "off"),
		summaryIs("rtk", "off — resolves off (project)"),
	)
	if errs := b.jsErrors(); len(errs) > 0 {
		t.Errorf("JS errors: %v", errs)
	}
}
