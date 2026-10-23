//go:build !windows

package web_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/sandboxsettings"
)

const (
	credClick = `((name) => { const b = [...document.querySelectorAll('#credential button')].find(b => b.checkVisibility() && b.textContent.trim() === name); b.click(); })`
	credText  = `document.querySelector('#credential').innerText`
)

// credPaste picks nothing: it types value into the password field, as a
// person pastes it, and leaves the kind to the page.
func credPaste(value string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const i = document.querySelector('#credential-value'); i.value = `+quote(value)+`; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}

func credKind(kind string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const r = document.querySelector('#credential input[name=credential-kind][value=`+kind+`]'); r.checked = true; r.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil)
}

// The agent credential, end to end: pasted in the browser, sealed in SQLite,
// shown as set and its kind, never its value.
func TestTheAgentCredentialIsPastedReplacedAndRemovedOnTheAgentIsolationPage_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	dir := t.TempDir()
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
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings, AgentCredential: cred})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings, SandboxSettings: &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"}, AgentCredential: cred}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	b := newBrowser(t)

	b.run("without a credential the section says agents cannot start",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`!document.querySelector('#credential [data-ssr]')`),
		waitUntil(credText+`.includes('No credential. Agents cannot start.')`),
	)
	b.run("a pasted OAuth token is saved, and the page shows set and its kind, not the value",
		chromedp.Evaluate(credClick+`('Paste a token or key')`, nil),
		waitUntil(`document.querySelector('#credential-value')?.checkVisibility() === true`),
		credPaste("sk-ant-oat01-first-secret"),
		waitUntil(`document.querySelector('#credential input[name=credential-kind][value=oauth]').checked`),
		chromedp.Evaluate(credClick+`('Save credential')`, nil),
		waitUntil(`document.querySelector('#credential-chip')?.innerText.trim() === 'Set'`),
		waitUntil(credText+`.includes('OAuth token')`),
		waitUntil(`document.querySelector('#credential-value') === null || !document.querySelector('#credential-value').checkVisibility()`),
	)
	b.run("the value is nowhere in the markup, before or after a reload",
		waitUntil(`!document.documentElement.outerHTML.includes('first-secret')`),
		chromedp.Reload(),
		waitUntil(`document.querySelector('#credential-chip')?.innerText.trim() === 'Set'`),
		waitUntil(`!document.documentElement.outerHTML.includes('first-secret')`),
		waitUntil(credText+`.includes('OAuth token')`),
	)
	if got := cred.Env(); len(got) != 1 || got[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-first-secret" {
		t.Errorf("stored credential env = %v", got)
	}

	b.run("replacing with an API key, the kind is preselected from the prefix and stays editable",
		chromedp.Evaluate(credClick+`('Replace with a pasted token or key')`, nil),
		waitUntil(`document.querySelector('#credential-value')?.checkVisibility() === true`),
		credPaste("sk-ant-api03-second-secret"),
		waitUntil(`document.querySelector('#credential input[name=credential-kind][value=api_key]').checked`),
		credKind("oauth"),
		credKind("api_key"),
		chromedp.Evaluate(credClick+`('Save credential')`, nil),
		waitUntil(credText+`.includes('Anthropic API key')`),
		waitUntil(`!document.documentElement.outerHTML.includes('second-secret')`),
	)
	if got := cred.Env(); len(got) != 1 || got[0] != "ANTHROPIC_API_KEY=sk-ant-api03-second-secret" {
		t.Errorf("replaced credential env = %v", got)
	}

	b.run("remove asks first, and removing leaves no credential",
		chromedp.Evaluate(credClick+`('Remove')`, nil),
		waitUntil(`document.querySelector('#credential [role=alertdialog]')?.checkVisibility() === true`),
		chromedp.Evaluate(credClick+`('Remove credential')`, nil),
		waitUntil(credText+`.includes('No credential. Agents cannot start.')`),
	)
	if cred.Status().Set || cred.Env() != nil {
		t.Errorf("the credential is still stored: %+v", cred.Status())
	}
	b.run("a refused save keeps the typed value and says why",
		chromedp.Evaluate(credClick+`('Paste a token or key')`, nil),
		waitUntil(`document.querySelector('#credential-value')?.checkVisibility() === true`),
		chromedp.Evaluate(credClick+`('Save credential')`, nil),
		waitUntil(`document.querySelector('#credential [role=alert]')?.innerText.includes('Paste a token or key.')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
