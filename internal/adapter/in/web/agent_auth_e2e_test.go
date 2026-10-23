//go:build !windows

package web_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// fakeAuthClaude answers `auth status` and `auth logout` from files in the
// agent home: the login file says signed in unless a "stale" file beside it
// says claude no longer accepts it; a "broken" file makes the status fail.
const fakeAuthClaude = `#!/bin/sh
h="$CLAUDE_CONFIG_DIR"
if [ "$1 $2" = "auth status" ]; then
  [ -f "$h/broken" ] && { echo "claude crashed"; exit 3; }
  if [ -f "$h/.credentials.json" ] && [ ! -f "$h/stale" ]; then
    echo '{"loggedIn": true, "authMethod": "claude.ai", "subscriptionType": "max", "email": "ana@example.com"}'
    exit 0
  fi
  echo '{"loggedIn": false, "authMethod": "none"}'
  exit 1
fi
if [ "$1 $2" = "auth logout" ]; then
  rm -f "$h/.credentials.json" "$h/stale"
  exit 0
fi
exit 2
`

const loginFileSecret = `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-filesecret"}}`

func authRig(t *testing.T, files ...string) (srv *httptest.Server, home string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte(fakeAuthClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		body := ""
		if f == ".credentials.json" {
			body = loginFileSecret
		}
		if err := os.WriteFile(filepath.Join(home, f), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
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
	cred := agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil)
	bin := fakeBin{path: script, installed: true}
	oauth := agentoauth.New(agentoauth.Deps{Bin: bin, ConfigDir: home, PTY: ptyunix.New()})
	t.Cleanup(oauth.Close)
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings, AgentCredential: cred, AgentOAuth: oauth, AgentAuth: oauth, ManagedClaudeBin: bin})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings, SandboxSettings: &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"}, AgentCredential: cred, AgentOAuth: oauth, AgentAuth: oauth, ManagedClaudeBin: bin}, builtAssets(t), nil))
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, home
}

const (
	// credHas is true when a visible button of the section carries name.
	credHas  = `((name) => [...document.querySelectorAll('#credential button')].some(b => b.checkVisibility() && b.textContent.trim() === name))`
	credChip = `document.querySelector('[data-chip=credential]').textContent.trim()`
)

func loaded() chromedp.Action {
	return waitUntil(`!document.querySelector('#credential [data-ssr]') && !!document.querySelector('#credential-seed')`)
}

// Signed out: Sign in with Claude and the secondary option, no account.
func TestTheCredentialSectionOffersSignInWhenSignedOutInTheBrowser(t *testing.T) {
	srv, _ := authRig(t)
	b := newBrowser(t)
	b.run("signed out shows the sign-in button, the secondary option, and no account",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		loaded(),
		waitUntil(credHas+`('Sign in with Claude')`),
		waitUntil(credText+`.includes('Not signed in')`),
		waitUntil(credHas+`('Use an API key or token instead')`),
		waitUntil(credHas+`('Paste a token or key')`),
		waitUntil(`!`+credHas+`('Sign out')`),
		waitUntil(`!`+credText+`.includes('ana@example.com')`),
		waitUntil(credChip+` === 'Credential: missing'`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Signed in: the account and subscription from `auth status`, Sign out, the
// secondary option collapsed; nothing of the login file anywhere in the page.
func TestTheCredentialSectionShowsTheSignedInAccountAndSignsOutInTheBrowser(t *testing.T) {
	srv, home := authRig(t, ".credentials.json")
	b := newBrowser(t)
	var html string
	b.run("signed in shows the account, the subscription and Sign out; the secondary option is collapsed",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		loaded(),
		waitUntil(credText+`.includes('ana@example.com')`),
		waitUntil(credText+`.includes('max')`),
		waitUntil(credHas+`('Sign out')`),
		waitUntil(`!`+credHas+`('Sign in with Claude')`),
		waitUntil(credHas+`('Use an API key or token instead')`),
		waitUntil(`!`+credHas+`('Paste a token or key')`),
		waitUntil(credChip+` === 'Credential: signed in'`),
		waitUntil(`!`+credText+`.includes('No credential')`),
		chromedp.OuterHTML("html", &html),
	)
	if strings.Contains(html, "filesecret") || strings.Contains(html, "claudeAiOauth") {
		t.Error("the page carries the login file's content")
	}
	b.run("the secondary option opens on a click",
		chromedp.Evaluate(credClick+`('Use an API key or token instead')`, nil),
		waitUntil(credHas+`('Paste a token or key')`),
	)
	b.run("Sign out runs logout and the section returns to signed out",
		chromedp.Evaluate(credClick+`('Sign out')`, nil),
		waitUntil(credHas+`('Sign in with Claude')`),
		waitUntil(`!`+credHas+`('Sign out')`),
		waitUntil(`!`+credText+`.includes('ana@example.com')`),
		waitUntil(credText+`.includes('Not signed in')`),
		waitUntil(credChip+` === 'Credential: missing'`),
	)
	if _, err := os.Stat(filepath.Join(home, ".credentials.json")); err == nil {
		t.Error("sign out left the login in the agent home")
	}
	b.run("a reload still says signed out",
		chromedp.Reload(),
		loaded(),
		waitUntil(credText+`.includes('Not signed in')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A status that fails is "could not check", and the page still renders.
func TestAFailingAuthStatusShowsCouldNotCheckInTheBrowser(t *testing.T) {
	srv, _ := authRig(t, ".credentials.json", "broken")
	b := newBrowser(t)
	b.run("could not check, with the rest of the section in place",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		loaded(),
		waitUntil(credText+`.includes('Could not check')`),
		waitUntil(credHas+`('Sign in with Claude')`),
		waitUntil(credHas+`('Paste a token or key')`),
		waitUntil(`!`+credText+`.includes('ana@example.com')`),
		waitUntil(`!`+credText+`.includes('Not signed in')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A login file that claude no longer accepts is not a sign-in: the status
// comes from `auth status`, never from the file.
func TestAStaleLoginFileShowsAsNotSignedInInTheBrowser(t *testing.T) {
	srv, _ := authRig(t, ".credentials.json", "stale")
	b := newBrowser(t)
	var html string
	b.run("a stale file is not signed in",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		loaded(),
		waitUntil(credText+`.includes('Not signed in')`),
		waitUntil(credHas+`('Sign in with Claude')`),
		waitUntil(`!`+credHas+`('Sign out')`),
		waitUntil(credChip+` === 'Credential: missing'`),
		chromedp.OuterHTML("html", &html),
	)
	if strings.Contains(html, "filesecret") {
		t.Error("the page carries the login file's content")
	}
}
