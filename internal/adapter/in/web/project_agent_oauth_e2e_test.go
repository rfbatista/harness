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
	"time"

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
	"operators-mcp/internal/domain"
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
if [ "$1 $2" = "auth logout" ] && [ -f "$(dirname "$0")/logout-fails" ]; then echo "logout refused" >&2; exit 1; fi
if [ "$1 $2" = "auth logout" ]; then echo "$CLAUDE_CONFIG_DIR" >> "$(dirname "$0")/logouts"; rm -f "$CLAUDE_CONFIG_DIR/.credentials.json"; exit 0; fi
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

// projectSignInRig is the server of a project's Agent credential section with
// two projects, a fake claude, and the global and project sign-ins beside the
// credential service.
type projectSignInRig struct {
	srv         *httptest.Server
	cred        *agentcredential.Service
	projects    *agentoauth.Projects
	alpha, beta *domain.Project
	homes       string
	globalHome  string
	// script is the fake claude; a logout it is asked for is logged beside it.
	script string
}

func newProjectSignInRig(t *testing.T) *projectSignInRig {
	t.Helper()
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
	r := &projectSignInRig{cred: cred, script: script, homes: filepath.Join(dir, "claude-homes"), globalHome: filepath.Join(dir, "home")}
	global := agentoauth.New(agentoauth.Deps{Bin: bin, ConfigDir: r.globalHome, PTY: ptyunix.New()})
	t.Cleanup(global.Close)
	r.projects = agentoauth.NewProjects(agentoauth.ProjectsDeps{Deps: agentoauth.Deps{Bin: bin, PTY: ptyunix.New()}, HomesDir: r.homes,
		OnDone: func(projectID string) { _, _ = cred.RemoveFor(projectID) }})
	t.Cleanup(r.projects.Close)
	r.projects.Subscribe(cat.Bus)
	cred.WithLogins(r.projects, global, func() bool {
		_, err := os.Stat(filepath.Join(r.globalHome, ".credentials.json"))
		return err == nil
	})
	if r.alpha, err = cat.Projects.CreateProject(ctx, "alpha", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if r.beta, err = cat.Projects.CreateProject(ctx, "beta", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan, Settings: cat.Settings, AgentCredential: cred, ProjectAgentCredentials: cred,
		AgentOAuth: global, AgentAuth: global, ProjectAgentOAuth: r.projects, ManagedClaudeBin: bin})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, ProjectSummaries: cat.Projects, Settings: cat.Settings, AgentCredential: cred, ProjectAgentCredentials: cred,
		AgentOAuth: global, AgentAuth: global, ProjectAgentOAuth: r.projects, ManagedClaudeBin: bin}, builtAssets(t), nil))
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// Sign in with Claude for one project, end to end against a fake `claude auth
// login`: the flow keeps its login in the project Claude home, shows the
// account it is signed in as, and runs independently of other projects' flows
// and of the global one.
func TestAProjectSignsInWithClaudeOnItsSettingsPage_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	srv, alpha, beta, homes, globalHome, projects := r.srv, r.alpha, r.beta, r.homes, r.globalHome, r.projects
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

const (
	poDialog = `document.querySelector('#project-credential-replace')`
	poChip   = `document.querySelector('#project-credential-chip')?.innerText.trim()`
)

// poSignIn runs the project's sign-in with the right code, as a person does.
func poSignIn() []chromedp.Action {
	return []chromedp.Action{
		waitUntil(`document.querySelector('#project-credential a[href*="oauth/authorize"]')?.checkVisibility() === true`),
		poTypeCode("good-code"),
		chromedp.Evaluate(poClick+`('Save code')`, nil),
		waitUntil(`document.querySelector('#project-credential [role=group]')?.checkVisibility() !== true`),
	}
}

// A project holds one project credential at most, and says what it falls back
// to while it holds none; signing out asks first.
func TestAProjectHoldsOneCredentialAtATimeAndNamesTheOneItFallsBackTo_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	b := newBrowser(t)
	page := r.srv.URL + "/projects/" + r.alpha.ID + "/settings"

	b.run("with nothing set anywhere the section says agents cannot start",
		chromedp.Navigate(page),
		waitUntil(`!document.querySelector('#project-credential [data-ssr]')`),
		waitUntil(poText+`.includes('No credential: agents cannot start')`),
	)
	if _, err := r.cred.Set("oauth", "sk-ant-oat01-global-secret"); err != nil {
		t.Fatal(err)
	}
	b.run("a global pasted credential is named",
		chromedp.Reload(),
		waitUntil(poText+`.includes('Uses the global pasted OAuth token')`),
	)
	if err := os.MkdirAll(r.globalHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.globalHome, ".credentials.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b.run("the global login comes first, with its account and subscription",
		chromedp.Reload(),
		waitUntil(poText+`.includes('Uses the global login: ana@example.com, max')`),
	)

	b.run("a pasted API key becomes the project credential, and the fallback line goes",
		chromedp.Evaluate(poClick+`('Paste a token or key')`, nil),
		waitUntil(`document.querySelector('#project-credential-value')?.checkVisibility() === true`),
		pcPaste("sk-ant-api03-alpha-secret"),
		chromedp.Evaluate(pcClick+`('Save credential')`, nil),
		waitUntil(poChip+` === 'Own credential'`),
		waitUntil(`!`+poText+`.includes('Uses the global')`),
	)
	b.run("signing in over it asks first; keeping it starts nothing",
		chromedp.Evaluate(poClick+`('Sign in with Claude')`, nil),
		waitUntil(poDialog+`?.checkVisibility() === true`),
		waitUntil(poDialog+`.innerText.includes('replaces the pasted credential')`),
		chromedp.Evaluate(poClick+`('Keep it')`, nil),
		waitUntil(poDialog+`?.checkVisibility() !== true`),
	)
	if st, _ := r.cred.StatusFor(r.alpha.ID); !st.Set || st.Kind != "api_key" {
		t.Errorf("declining changed the pasted credential: %+v", st)
	}
	if _, got := apiBody(t, r.srv.URL+"/api/get_project_agent_oauth?project_id="+r.alpha.ID); !strings.Contains(got, `"idle"`) {
		t.Errorf("declining started a flow: %s", got)
	}
	b.run("confirming signs in, and the login replaces the pasted credential",
		chromedp.Evaluate(poClick+`('Sign in with Claude')`, nil),
		waitUntil(poDialog+`?.checkVisibility() === true`),
		chromedp.Evaluate(poClick+`('Sign in and replace')`, nil),
	)
	b.run("the right code finishes it", poSignIn()...)
	b.run("the section shows the login only",
		waitUntil(poChip+` === 'Signed in'`),
		waitUntil(poText+`.includes('ana@example.com')`),
		waitUntil(`!`+poText+`.includes('Anthropic API key')`),
	)
	if st, _ := r.cred.StatusFor(r.alpha.ID); st.Set {
		t.Errorf("the pasted credential outlived the sign-in: %+v", st)
	}
	_, body := apiBody(t, r.srv.URL+"/api/get_project_agent_credential?project_id="+r.alpha.ID)
	for _, want := range []string{`"kind":"login"`, `"account":"ana@example.com"`, `"source":"project"`} {
		if !strings.Contains(body, want) {
			t.Errorf("status %s lacks %s", body, want)
		}
	}
	if strings.Contains(body, "browsersecret") {
		t.Errorf("status carries login content: %s", body)
	}

	b.run("sign out asks first and says running sessions lose access",
		chromedp.Evaluate(poClick+`('Sign out')`, nil),
		waitUntil(poDialog+`?.checkVisibility() === true`),
		waitUntil(poDialog+`.innerText.includes('sessions lose access when claude next refreshes')`),
		chromedp.Evaluate(poClick+`('Keep it')`, nil),
		waitUntil(poChip+` === 'Signed in'`),
	)
	if !r.projects.HasLogin(r.alpha.ID) {
		t.Error("declining the sign-out signed the project out")
	}
	b.run("confirming signs the project out: it falls back to the global login again",
		chromedp.Evaluate(poClick+`('Sign out')`, nil),
		waitUntil(poDialog+`?.checkVisibility() === true`),
		chromedp.Evaluate(poClick+`('Sign out of Claude')`, nil),
		waitUntil(poChip+` === 'Global'`),
		waitUntil(poText+`.includes('Uses the global login: ana@example.com, max')`),
	)
	if r.projects.HasLogin(r.alpha.ID) {
		t.Error("the project Claude home kept its login after the sign-out")
	}

	b.run("sign in again, then pasting over the login asks first",
		chromedp.Evaluate(poClick+`('Sign in with Claude')`, nil),
	)
	b.run("", poSignIn()...)
	b.run("pasting over the login",
		waitUntil(poChip+` === 'Signed in'`),
		chromedp.Evaluate(poClick+`('Paste a token or key')`, nil),
		waitUntil(poDialog+`?.checkVisibility() === true`),
		waitUntil(poDialog+`.innerText.includes('signs this project out')`),
		chromedp.Evaluate(poClick+`('Keep it')`, nil),
		waitUntil(poDialog+`?.checkVisibility() !== true`),
	)
	if !r.projects.HasLogin(r.alpha.ID) {
		t.Error("declining the paste signed the project out")
	}
	b.run("confirming signs the login out and saves the pasted credential",
		chromedp.Evaluate(poClick+`('Paste a token or key')`, nil),
		waitUntil(poDialog+`?.checkVisibility() === true`),
		chromedp.Evaluate(poClick+`('Replace login')`, nil),
		waitUntil(`document.querySelector('#project-credential-value')?.checkVisibility() === true`),
		pcPaste("sk-ant-api03-second-secret"),
		chromedp.Evaluate(pcClick+`('Save credential')`, nil),
		waitUntil(poChip+` === 'Own credential'`),
		waitUntil(poText+`.includes('Anthropic API key')`),
	)
	if r.projects.HasLogin(r.alpha.ID) {
		t.Error("the login outlived the pasted credential")
	}
	if st, _ := r.cred.StatusFor(r.alpha.ID); !st.Set || st.Kind != "api_key" {
		t.Errorf("the pasted credential was not kept: %+v", st)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// apiBody is the body of a GET /api call.
func apiBody(t *testing.T, url string) (int, string) {
	t.Helper()
	return apiAnswer(t, http.MethodGet, url, "")
}

// signInThroughTheAPI signs a project in with the fake claude's good code.
func signInThroughTheAPI(t *testing.T, srv *httptest.Server, projectID string) {
	t.Helper()
	body := `{"project_id":"` + projectID + `"`
	if code, out := apiAnswer(t, http.MethodPost, srv.URL+"/api/start_project_agent_oauth", body+`}`); code != http.StatusOK {
		t.Fatalf("start: %d %s", code, out)
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if _, out := apiBody(t, srv.URL+"/api/get_project_agent_oauth?project_id="+projectID); strings.Contains(out, "waiting_for_code") {
			break
		}
	}
	if code, out := apiAnswer(t, http.MethodPost, srv.URL+"/api/submit_project_agent_oauth_code", body+`,"code":"good-code"}`); code != http.StatusOK {
		t.Fatalf("submit: %d %s", code, out)
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if _, out := apiBody(t, srv.URL+"/api/get_project_agent_oauth?project_id="+projectID); strings.Contains(out, `"idle"`) || strings.Contains(out, `"done"`) {
			return
		}
	}
	t.Fatalf("the sign-in of %s did not finish", projectID)
}

// Deleting a project removes its project Claude home, and no other's. It does
// not run `claude auth logout`: the login is not revoked.
func TestDeletingAProjectRemovesItsClaudeHome_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	signInThroughTheAPI(t, r.srv, r.alpha.ID)
	signInThroughTheAPI(t, r.srv, r.beta.ID)
	if !r.projects.HasLogin(r.alpha.ID) || !r.projects.HasLogin(r.beta.ID) {
		t.Fatal("both projects should hold a login before the delete")
	}

	if code, out := apiAnswer(t, http.MethodPost, r.srv.URL+"/api/delete_project", `{"project_id":"`+r.alpha.ID+`"}`); code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(r.homes, r.alpha.ID)); !os.IsNotExist(err) {
		t.Errorf("the deleted project's Claude home is still there: %v", err)
	}
	if !r.projects.HasLogin(r.beta.ID) {
		t.Error("another project's login went with it")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.script), "logouts")); err == nil {
		t.Error("deleting the project ran claude auth logout")
	}
}

// A logout claude refuses is AGENT_SIGN_OUT_FAILED, and a pasted credential
// is not saved over a login that could not be signed out.
func TestAFailedLogoutKeepsTheProjectLoginAndSavesNothing_EndToEnd(t *testing.T) {
	r := newProjectSignInRig(t)
	signInThroughTheAPI(t, r.srv, r.alpha.ID)
	if err := os.WriteFile(filepath.Join(filepath.Dir(r.script), "logout-fails"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	code, body := apiAnswer(t, http.MethodPost, r.srv.URL+"/api/sign_out_project_agent_auth", `{"project_id":"`+r.alpha.ID+`"}`)
	if code < 400 || !strings.Contains(body, "AGENT_SIGN_OUT_FAILED") {
		t.Errorf("sign-out answered %d %s, want AGENT_SIGN_OUT_FAILED", code, body)
	}
	code, body = apiAnswer(t, http.MethodPost, r.srv.URL+"/api/set_project_agent_credential", `{"project_id":"`+r.alpha.ID+`","kind":"api_key","value":"sk-ant-api03-x"}`)
	if code < 400 || !strings.Contains(body, "AGENT_SIGN_OUT_FAILED") {
		t.Errorf("paste over a login that stays answered %d %s", code, body)
	}
	if st, _ := r.cred.StatusFor(r.alpha.ID); st.Set {
		t.Errorf("a pasted credential was saved beside the login: %+v", st)
	}
	if !r.projects.HasLogin(r.alpha.ID) {
		t.Error("the login went despite the failed logout")
	}
}
