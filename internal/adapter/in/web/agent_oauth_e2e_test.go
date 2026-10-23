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

// fakeClaude stands in for `claude auth login --claudeai` and `claude auth
// status`: the right code keeps a login in the agent home, which the status
// then reports.
const fakeClaude = `#!/bin/sh
dir=$(dirname "$0")
if [ "$1 $2" = "auth status" ]; then
  if [ -f "$CLAUDE_CONFIG_DIR/.credentials.json" ]; then
    echo '{"loggedIn": true, "authMethod": "claude.ai"}'
    exit 0
  fi
  echo '{"loggedIn": false}'
  exit 1
fi
[ "$1 $2 $3" = "auth login --claudeai" ] || exit 2
echo $$ > "$dir/pid"
echo "Opening browser to sign in..."
echo "If the browser didn't open, visit: https://claude.ai/oauth/authorize?state=browser-flow"
printf "Paste code here if prompted > "
read code
if [ "$code" != "good-code" ]; then
  echo "invalid code"
  exit 1
fi
echo '{"token":"sk-ant-oat01-browsersecret"}' > "$CLAUDE_CONFIG_DIR/.credentials.json"
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
	if err := os.WriteFile(script, []byte(fakeClaude), 0o755); err != nil {
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
	oauth := agentoauth.New(agentoauth.Deps{Bin: bin, ConfigDir: filepath.Join(dir, "home"), PTY: ptyunix.New()})
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
// auth login`: the URL appears, a reload resumes the flow, the right code
// signs claude in (the harness stores nothing), cancel and a failure change
// nothing.
func TestSignInWithClaudeInTheBrowser_EndToEnd(t *testing.T) {
	srv, cred, dir := oauthRig(t, true)
	home := filepath.Join(dir, "home")
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
	if _, err := os.Stat(filepath.Join(home, ".credentials.json")); err == nil {
		t.Error("a failed flow kept a login")
	}
	b.run("start again, paste the right code: the panel confirms the sign-in and closes",
		chromedp.Evaluate(credClick+`('Start again')`, nil),
		waitUntil(`document.querySelector('#credential a[href*="oauth/authorize"]')?.checkVisibility() === true`),
		oauthTypeCode("good-code"),
		chromedp.Evaluate(credClick+`('Save code')`, nil),
		waitUntil(credText+`.includes('Signed in.')`),
		waitUntil(`document.querySelector('#credential [role=group]')?.checkVisibility() !== true`),
		waitUntil(`!document.documentElement.outerHTML.includes('browsersecret')`),
	)
	if cred.Status().Set {
		t.Error("the harness stored a credential for the login")
	}
	if _, err := os.Stat(filepath.Join(home, ".credentials.json")); err != nil {
		t.Errorf("claude kept no login in the agent home: %v", err)
	}
	if !processGone(dir) {
		t.Error("the process outlived the flow")
	}
	b.run("Sign in again, then Cancel: the panel goes",
		chromedp.Evaluate(credClick+`('Sign in with Claude')`, nil),
		waitUntil(`document.querySelector('#credential a[href*="oauth/authorize"]')?.checkVisibility() === true`),
		chromedp.Evaluate(credClick+`('Cancel sign-in')`, nil),
		waitUntil(`document.querySelector('#credential [role=group]')?.checkVisibility() !== true`),
	)
	if !processGone(dir) {
		t.Error("cancel left the process running")
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
