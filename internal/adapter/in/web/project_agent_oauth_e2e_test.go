//go:build !windows

package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
)

// projectFakeClaude stands in for `claude auth login --claudeai` and `claude
// auth status` in a project Claude home: the right code keeps a login in the
// home named by CLAUDE_CONFIG_DIR, which the status then reports with an account.
const projectFakeClaude = `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  if [ -f "$CLAUDE_CONFIG_DIR/.credentials.json" ]; then
    echo '{"loggedIn": true, "authMethod": "claude.ai", "email": "ana@example.com", "subscriptionType": "max"}'
    exit 0
  fi
  echo '{"loggedIn": false}'
  exit 1
fi
if [ "$1 $2" = "auth logout" ]; then rm -f "$CLAUDE_CONFIG_DIR/.credentials.json"; exit 0; fi
[ "$1 $2 $3" = "auth login --claudeai" ] || exit 2
echo $$ > "$CLAUDE_CONFIG_DIR/pid"
echo "If the browser didn't open, visit: https://claude.ai/oauth/authorize?state=project-flow"
printf "Paste code here if prompted > "
read code
if [ "$code" != "good-code" ]; then
  echo "invalid code"
  exit 1
fi
echo '{"token":"sk-ant-oat01-browsersecret"}' > "$CLAUDE_CONFIG_DIR/.credentials.json"
`

const (
	poClick = `((name) => { const b = [...document.querySelectorAll('#project-credential button')].find(b => b.checkVisibility() && b.textContent.trim() === name); b.click(); })`
	poText  = `document.querySelector('#project-credential').innerText`
)

func poTypeCode(code string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const i = document.querySelector('#project-credential-oauth-code'); i.value = `+quote(code)+`; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}

// apiAnswer is the status and body of an /api call.
func apiAnswer(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// Sign in with Claude for one project, end to end against a fake `claude auth
// login`: the flow keeps its login in the project Claude home, shows the
// account it is signed in as, and runs independently of other projects' flows
// and of the global one.
func TestAProjectSignsInWithClaudeOnItsSettingsPage_EndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte(projectFakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
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
	box, err := secretbox.New("", filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	cred := agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil).WithProjects(sqlite.NewProjectAgentCredentialRepository(db))
	bin := fakeBin{path: script, installed: true}
	globalHome := filepath.Join(dir, "home")
	global := agentoauth.New(agentoauth.Deps{Bin: bin, ConfigDir: globalHome, PTY: ptyunix.New()})
	t.Cleanup(global.Close)
	homes := filepath.Join(dir, "claude-homes")
	projects := agentoauth.NewProjects(agentoauth.ProjectsDeps{Deps: agentoauth.Deps{Bin: bin, PTY: ptyunix.New()}, HomesDir: homes})
	t.Cleanup(projects.Close)

	alpha, err := cat.Projects.CreateProject(ctx, "alpha", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	beta, err := cat.Projects.CreateProject(ctx, "beta", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan, Settings: cat.Settings, AgentCredential: cred, ProjectAgentCredentials: cred,
		AgentOAuth: global, AgentAuth: global, ProjectAgentOAuth: projects, ManagedClaudeBin: bin})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, ProjectSummaries: cat.Projects, Settings: cat.Settings, AgentCredential: cred, ProjectAgentCredentials: cred,
		AgentOAuth: global, AgentAuth: global, ProjectAgentOAuth: projects, ManagedClaudeBin: bin}, builtAssets(t), nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	b := newBrowser(t)

	b.run("the Agent credential section offers Sign in with Claude beside Paste",
		chromedp.Navigate(srv.URL+"/projects/"+alpha.ID+"/settings"),
		waitUntil(`!document.querySelector('#project-credential [data-ssr]')`),
		waitUntil(poText+`.includes('Sign in with Claude')`),
		waitUntil(poText+`.includes('Paste a token or key')`),
		waitUntil(`document.querySelector('#project-credential [role=group]')?.checkVisibility() !== true`),
	)
	b.run("starting shows the authorization URL as a link and as text, with the time left",
		chromedp.Evaluate(poClick+`('Sign in with Claude')`, nil),
		waitUntil(`document.querySelector('#project-credential [role=group]')?.checkVisibility() === true`),
		waitUntil(`document.querySelector('#project-credential a[href*="oauth/authorize"]')?.getAttribute('href') === 'https://claude.ai/oauth/authorize?state=project-flow'`),
		waitUntil(poText+`.includes('https://claude.ai/oauth/authorize?state=project-flow')`),
		waitUntil(poText+`.includes('left')`),
	)
	b.run("reloading mid-flow finds the same pending flow, and a second start is not offered",
		chromedp.Reload(),
		waitUntil(`document.querySelector('#project-credential a[href*="oauth/authorize"]')?.getAttribute('href') === 'https://claude.ai/oauth/authorize?state=project-flow'`),
		waitUntil(`[...document.querySelectorAll('#project-credential button')].find(b => b.textContent.trim() === 'Sign in with Claude').disabled`),
	)

	if code, body := apiAnswer(t, http.MethodPost, srv.URL+"/api/start_project_agent_oauth", `{"project_id":"`+alpha.ID+`"}`); code < 400 || !strings.Contains(body, "AGENT_OAUTH_BUSY") {
		t.Errorf("a second start for the same project answered %d %s, want AGENT_OAUTH_BUSY", code, body)
	}
	if code, body := apiAnswer(t, http.MethodPost, srv.URL+"/api/start_project_agent_oauth", `{"project_id":"`+beta.ID+`"}`); code != http.StatusOK {
		t.Errorf("another project's start answered %d %s, want it to run beside alpha's", code, body)
	}
	if code, body := apiAnswer(t, http.MethodPost, srv.URL+"/api/start_agent_oauth", `{}`); code != http.StatusOK {
		t.Errorf("the global start answered %d %s, want it to run beside the projects'", code, body)
	}
	if code, body := apiAnswer(t, http.MethodPost, srv.URL+"/api/start_project_agent_oauth", `{"project_id":"nope"}`); code < 400 || !strings.Contains(body, "PROJECT_NOT_FOUND") {
		t.Errorf("an unknown project answered %d %s, want PROJECT_NOT_FOUND", code, body)
	}

	b.run("a wrong code ends alpha's flow, saves nothing and leaves beta's flow running",
		poTypeCode("bad-code"),
		chromedp.Evaluate(poClick+`('Save code')`, nil),
		waitUntil(`document.querySelector('#project-credential-oauth-failed')?.innerText.includes('Nothing was saved')`),
		waitUntil(poText+`.includes('invalid code')`),
	)
	if _, err := os.Stat(filepath.Join(homes, alpha.ID, ".credentials.json")); err == nil {
		t.Error("a failed flow kept a login")
	}
	if _, body := apiAnswer(t, http.MethodGet, srv.URL+"/api/get_project_agent_oauth?project_id="+beta.ID, ""); !strings.Contains(body, "waiting_for_code") {
		t.Errorf("beta's flow was disturbed by alpha's: %s", body)
	}

	b.run("start again and paste the right code: the section shows the account and subscription",
		chromedp.Evaluate(poClick+`('Start again')`, nil),
		waitUntil(`document.querySelector('#project-credential a[href*="oauth/authorize"]')?.checkVisibility() === true`),
		poTypeCode("good-code"),
		chromedp.Evaluate(poClick+`('Save code')`, nil),
		waitUntil(poText+`.includes('ana@example.com')`),
		waitUntil(poText+`.includes('max')`),
		waitUntil(`document.querySelector('#project-credential [role=group]')?.checkVisibility() !== true`),
		waitUntil(`!document.documentElement.outerHTML.includes('browsersecret')`),
	)
	if _, err := os.Stat(filepath.Join(homes, alpha.ID, ".credentials.json")); err != nil {
		t.Errorf("claude kept no login in alpha's project Claude home: %v", err)
	}
	for _, other := range []string{filepath.Join(homes, beta.ID), globalHome} {
		if _, err := os.Stat(filepath.Join(other, ".credentials.json")); err == nil {
			t.Errorf("a login reached %s", other)
		}
	}
	if projects.HasLogin(beta.ID) || !projects.HasLogin(alpha.ID) {
		t.Error("the login status is not per project")
	}
	_, status := apiAnswer(t, http.MethodGet, srv.URL+"/api/get_project_agent_auth_status?project_id="+alpha.ID, "")
	if !strings.Contains(status, `"account":"ana@example.com"`) || strings.Contains(status, "browsersecret") {
		t.Errorf("status = %s", status)
	}

	b.run("cancelling beta's flow closes its panel and keeps nothing",
		chromedp.Navigate(srv.URL+"/projects/"+beta.ID+"/settings"),
		waitUntil(`document.querySelector('#project-credential a[href*="oauth/authorize"]')?.checkVisibility() === true`),
		chromedp.Evaluate(poClick+`('Cancel sign-in')`, nil),
		waitUntil(`document.querySelector('#project-credential [role=group]')?.checkVisibility() !== true`),
		waitUntil(poText+`.includes('Sign in with Claude')`),
	)
	if _, err := os.Stat(filepath.Join(homes, beta.ID, ".credentials.json")); err == nil {
		t.Error("a cancelled flow kept a login")
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
