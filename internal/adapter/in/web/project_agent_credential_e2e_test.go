//go:build !windows

package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
)

const (
	pcClick = `((name) => { const b = [...document.querySelectorAll('#project-credential button')].find(b => b.checkVisibility() && b.textContent.trim() === name); b.click(); })`
	pcText  = `document.querySelector('#project-credential').innerText`
)

// pcPaste types value into the project's password field, as a person pastes it.
func pcPaste(value string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const i = document.querySelector('#project-credential-value'); i.value = `+quote(value)+`; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}

// A project's own agent credential, end to end: pasted on its settings page,
// sealed in SQLite, used for that project's sessions only, never shown again.
func TestAProjectsOwnAgentCredentialIsPastedAndRemovedOnItsSettingsPage_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	ctx := context.Background()
	var (
		cat  catalog.Catalog
		orch *orchestration.Service
		plan *planning.Service
		db   *gorm.DB
	)
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: t.TempDir(), ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&cat, &orch, &plan, &db),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })
	box, err := secretbox.New("", filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	cred := agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil).WithProjects(sqlite.NewProjectAgentCredentialRepository(db))
	alpha, err := cat.Projects.CreateProject(ctx, "alpha", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	beta, err := cat.Projects.CreateProject(ctx, "beta", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cred.Set("oauth", "sk-ant-oat01-global-secret"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan, Settings: cat.Settings, AgentCredential: cred, ProjectAgentCredentials: cred})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, ProjectSummaries: cat.Projects, Settings: cat.Settings, AgentCredential: cred, ProjectAgentCredentials: cred}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	b := newBrowser(t)

	b.run("a project without its own credential says it uses the global one",
		chromedp.Navigate(srv.URL+"/projects/"+alpha.ID+"/settings"),
		waitUntil(`!document.querySelector('#project-credential [data-ssr]')`),
		waitUntil(`document.querySelector('#project-credential-chip')?.innerText.trim() === 'Global'`),
		waitUntil(pcText+`.includes('use the global Agent credential')`),
	)
	b.run("a pasted API key is saved for this project; the kind is shown, not the value",
		chromedp.Evaluate(pcClick+`('Paste a token or key')`, nil),
		waitUntil(`document.querySelector('#project-credential-value')?.checkVisibility() === true`),
		pcPaste("sk-ant-api03-alpha-secret"),
		waitUntil(`document.querySelector('#project-credential input[name=project-credential-kind][value=api_key]').checked`),
		chromedp.Evaluate(pcClick+`('Save credential')`, nil),
		waitUntil(`document.querySelector('#project-credential-chip')?.innerText.trim() === 'Own credential'`),
		waitUntil(pcText+`.includes('Anthropic API key')`),
		waitUntil(`!document.documentElement.outerHTML.includes('alpha-secret')`),
	)
	b.run("after a reload it is still set, and the value is nowhere in the markup",
		chromedp.Reload(),
		waitUntil(`document.querySelector('#project-credential-chip')?.innerText.trim() === 'Own credential'`),
		waitUntil(`!document.documentElement.outerHTML.includes('alpha-secret')`),
	)
	if got := cred.EnvFor(alpha.ID); len(got) != 1 || got[0] != "ANTHROPIC_API_KEY=sk-ant-api03-alpha-secret" {
		t.Errorf("alpha env = %v", got)
	}
	if got := cred.EnvFor(beta.ID); len(got) != 1 || got[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-global-secret" {
		t.Errorf("beta env = %v, want the global credential", got)
	}

	b.run("another project's page is unaffected",
		chromedp.Navigate(srv.URL+"/projects/"+beta.ID+"/settings"),
		waitUntil(`document.querySelector('#project-credential-chip')?.innerText.trim() === 'Global'`),
	)
	b.run("remove asks first, and removing returns the project to the global credential",
		chromedp.Navigate(srv.URL+"/projects/"+alpha.ID+"/settings"),
		waitUntil(`document.querySelector('#project-credential-chip')?.innerText.trim() === 'Own credential'`),
		chromedp.Evaluate(pcClick+`('Remove')`, nil),
		waitUntil(`document.querySelector('#project-credential [role=alertdialog]')?.checkVisibility() === true`),
		chromedp.Evaluate(pcClick+`('Remove credential')`, nil),
		waitUntil(`document.querySelector('#project-credential-chip')?.innerText.trim() === 'Global'`),
	)
	if got := cred.EnvFor(alpha.ID); len(got) != 1 || got[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-global-secret" {
		t.Errorf("alpha env after removal = %v", got)
	}
	b.run("a refused save keeps the form open and says why",
		chromedp.Evaluate(pcClick+`('Paste a token or key')`, nil),
		waitUntil(`document.querySelector('#project-credential-value')?.checkVisibility() === true`),
		chromedp.Evaluate(pcClick+`('Save credential')`, nil),
		waitUntil(`document.querySelector('#project-credential [role=alert]')?.innerText.includes('Paste a token or key.')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
