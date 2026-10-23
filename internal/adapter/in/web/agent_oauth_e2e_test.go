//go:build !windows

package web_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/application/sandboxsettings"
)

// fakeSetupToken stands in for `claude setup-token`.
const fakeSetupToken = `#!/bin/sh
dir=$(dirname "$0")
echo $$ > "$dir/pid"
echo "https://claude.ai/oauth/authorize?state=browser-flow"
printf "Paste the code here: "
read code
if [ "$code" != "good-code" ]; then
  echo "invalid code"
  exit 1
fi
echo "sk-ant-oat01-browsersecret"
`

type fakeBin struct {
	path      string
	installed bool
}

func (b fakeBin) Bin() string      { return b.path }
func (b fakeBin) Installed() bool  { return b.installed }
func (b fakeBin) Overridden() bool { return false }

const oauthCode = `document.querySelector('#credential-oauth-code')`

func oauthTypeCode(code string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const i = `+oauthCode+`; i.value = `+quote(code)+`; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}

func oauthRig(t *testing.T, installed bool) (srv *httptest.Server, cred *agentcredential.Service, dir string) {
	t.Helper()
	dir = t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte(fakeSetupToken), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	box, err := secretbox.New("", filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(catalog.SQLiteDeps(db))
	cred = agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil)
	bin := fakeBin{path: script, installed: installed}
	oauth := agentoauth.New(agentoauth.Deps{Bin: bin, ConfigDir: filepath.Join(dir, "home"), PTY: ptyunix.New(), Credential: cred})
	t.Cleanup(oauth.Close)
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings, AgentCredential: cred, AgentOAuth: oauth, ManagedClaudeBin: bin})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings, SandboxSettings: &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"}, AgentCredential: cred, AgentOAuth: oauth, ManagedClaudeBin: bin}, builtAssets(t), nil))
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, cred, dir
}

func processGone(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
	}
	return false
}

// The Sign in with Claude panel, end to end against a fake `claude
// setup-token`: the URL appears, a reload resumes the flow, the code stores
// the token, cancel and a failure change nothing.
func TestSignInWithClaudeInTheBrowser_EndToEnd(t *testing.T) {
	srv, cred, dir := oauthRig(t, true)
	b := newBrowser(t)

	b.run("the panel starts hidden and Sign in with Claude is offered",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`!document.querySelector('#credential [data-ssr]')`),
		waitUntil(`!document.querySelector('#credential [role=group]')?.checkVisibility()`),
		waitUntil(credText+`.includes('Sign in with Claude')`),
	)
	b.run("starting shows the authorization URL as a link and as text",
		chromedp.Evaluate(credClick+`('Sign in with Claude')`, nil),
		waitUntil(`document.querySelector('#credential [role=group]')?.checkVisibility() === true`),
		waitUntil(`document.querySelector('#credential a[href*="oauth/authorize"]')?.getAttribute('href') === 'https://claude.ai/oauth/authorize?state=browser-flow'`),
		waitUntil(credText+`.includes('https://claude.ai/oauth/authorize?state=browser-flow')`),
	)
	b.run("reloading mid-flow shows the same pending flow, and a second start is not offered",
		chromedp.Reload(),
		waitUntil(`document.querySelector('#credential [role=group]')?.checkVisibility() === true`),
		waitUntil(`document.querySelector('#credential a[href*="oauth/authorize"]')?.getAttribute('href') === 'https://claude.ai/oauth/authorize?state=browser-flow'`),
		waitUntil(`[...document.querySelectorAll('#credential button')].find(b => b.textContent.trim() === 'Sign in with Claude').disabled`),
	)
	b.run("a wrong code ends the flow with the process's last line and saves nothing",
		oauthTypeCode("bad-code"),
		chromedp.Evaluate(credClick+`('Save code')`, nil),
		waitUntil(`document.querySelector('#credential-oauth-failed')?.innerText.includes('Nothing was saved')`),
		waitUntil(credText+`.includes('invalid code')`),
	)
	if cred.Status().Set {
		t.Error("a failed flow stored a credential")
	}
	b.run("start again, paste the right code: the token is stored and the page shows it set",
		chromedp.Evaluate(credClick+`('Start again')`, nil),
		waitUntil(`document.querySelector('#credential a[href*="oauth/authorize"]')?.checkVisibility() === true`),
		oauthTypeCode("good-code"),
		chromedp.Evaluate(credClick+`('Save code')`, nil),
		waitUntil(`document.querySelector('#credential-chip')?.innerText.trim() === 'Set'`),
		waitUntil(credText+`.includes('Credential saved.')`),
		waitUntil(credText+`.includes('OAuth token')`),
		waitUntil(`document.querySelector('#credential [role=group]')?.checkVisibility() !== true`),
		waitUntil(`!document.documentElement.outerHTML.includes('browsersecret')`),
	)
	if env := cred.Env(); len(env) != 1 || env[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-browsersecret" {
		t.Errorf("stored credential env = %v", env)
	}
	if !processGone(dir) {
		t.Error("the process outlived the flow")
	}
	b.run("Sign in again, then Cancel: the panel goes and the credential stays",
		chromedp.Evaluate(credClick+`('Sign in again')`, nil),
		waitUntil(`document.querySelector('#credential a[href*="oauth/authorize"]')?.checkVisibility() === true`),
		chromedp.Evaluate(credClick+`('Cancel sign-in')`, nil),
		waitUntil(`document.querySelector('#credential [role=group]')?.checkVisibility() !== true`),
		waitUntil(`document.querySelector('#credential-chip')?.innerText.trim() === 'Set'`),
	)
	if !processGone(dir) {
		t.Error("cancel left the process running")
	}
	if env := cred.Env(); len(env) != 1 || env[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-browsersecret" {
		t.Errorf("cancel changed the credential: %v", env)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Without a managed claude there is nothing to run: the button is disabled
// and says why.
func TestSignInNeedsTheManagedClaudeInTheBrowser(t *testing.T) {
	srv, _, _ := oauthRig(t, false)
	b := newBrowser(t)
	b.run("the button is disabled and the hint points at the managed claude",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`!document.querySelector('#credential [data-ssr]')`),
		waitUntil(`[...document.querySelectorAll('#credential button')].find(b => b.textContent.trim() === 'Sign in with Claude')?.disabled === true`),
		waitUntil(credText+`.includes('Sign in needs the managed claude. Install it first.')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
