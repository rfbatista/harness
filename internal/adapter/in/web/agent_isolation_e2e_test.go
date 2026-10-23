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
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// agentIsolationServer serves the pages and the API over SQLite settings and
// the real sandbox settings store. driver is what the platform has (nil:
// none).
func agentIsolationServer(t *testing.T, driver ports.SandboxDriver) (*httptest.Server, *sandboxsettings.Store) {
	t.Helper()
	assets := builtAssets(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	store := &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"}
	current := func() ports.SandboxDriver { return driver }
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings, SandboxSettings: store, SandboxDriver: current})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings, SandboxSettings: store, SandboxDriver: current}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, store
}

const (
	isoSwitchOn  = `document.querySelector('#sandbox [role=switch]')?.getAttribute('aria-checked') === 'true'`
	isoSwitchOff = `document.querySelector('#sandbox [role=switch]')?.getAttribute('aria-checked') === 'false'`
	// isoPaths lists the rows as "path=access".
	isoPaths = `[...document.querySelectorAll('#sandbox .sandbox-path')].map(r => r.querySelector('.path').textContent.trim() + '=' + r.querySelector('select').value).join(',')`
	// isoClick clicks the visible button of #sandbox with the given name.
	isoClick = `((name) => { const b = [...document.querySelectorAll('#sandbox button')].find(b => b.checkVisibility() && (b.textContent.trim() === name || b.getAttribute('aria-label') === name)); b.click(); })`
)

func isoAdd(path, access string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const i = document.querySelector('#sandbox-new-path'); i.value = `+quote(path)+`; i.dispatchEvent(new Event('input', { bubbles: true }));
		const s = document.querySelector('#sandbox-new-access'); s.value = `+quote(access)+`; s.dispatchEvent(new Event('change', { bubbles: true }));
		`+isoClick+`('Add path'); })()`, nil)
}

// The Agent isolation page, end to end: the settings in SQLite, the API and
// the pages, and a person in headless Chrome.
func TestTheSandboxSwitchAndExtraPathsAreSetOnTheAgentIsolationPage_EndToEnd(t *testing.T) {
	srv, store := agentIsolationServer(t, &portstest.RecordingSandboxDriver{})
	b := newBrowser(t)

	b.run("the top bar links to the page",
		chromedp.Navigate(srv.URL+"/projects/new"),
		waitUntil(`document.querySelector('header a[aria-label="Agent isolation"]')?.getAttribute('href') === '/agent-isolation'`),
	)
	b.run("the page has the three sections to link to",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`['credential', 'claude', 'sandbox'].every(id => document.getElementById(id)?.tagName === 'SECTION')`),
		waitUntil(isoSwitchOn),
		waitUntil(`!document.querySelector('#sandbox [data-ssr]')`),
	)

	b.run("turning the sandbox off asks first, and Keep it on leaves it on",
		chromedp.Evaluate(`document.querySelector('#sandbox [role=switch]').click()`, nil),
		waitUntil(`document.querySelector('#sandbox [role=alertdialog]')?.checkVisibility() === true`),
		chromedp.Evaluate(isoClick+`('Keep it on')`, nil),
		waitUntil(`document.querySelector('#sandbox [role=alertdialog]')?.checkVisibility() === false`),
		waitUntil(isoSwitchOn),
	)
	b.run("confirming turns it off and the page says so",
		chromedp.Evaluate(`document.querySelector('#sandbox [role=switch]').click()`, nil),
		waitUntil(`document.querySelector('#sandbox [role=alertdialog]')?.checkVisibility() === true`),
		chromedp.Evaluate(isoClick+`('Turn sandbox off')`, nil),
		waitUntil(isoSwitchOff),
		waitUntil(`document.querySelector('#sandbox').innerText.includes('Sandbox is off.')`),
	)
	b.run("it stays off across a reload",
		chromedp.Reload(),
		waitUntil(isoSwitchOff),
		waitUntil(`document.querySelector('#sandbox').innerText.includes('Sandbox is off.')`),
	)
	if store.Enabled() {
		t.Error("the store still has the sandbox on")
	}
	b.run("turning it back on needs no confirmation",
		chromedp.Evaluate(`document.querySelector('#sandbox [role=switch]').click()`, nil),
		waitUntil(isoSwitchOn),
		waitUntil(`!document.querySelector('#sandbox').innerText.includes('Sandbox is off.')`),
	)
	if !store.Enabled() {
		t.Error("the store still has the sandbox off")
	}

	b.run("the empty list says what agents get",
		waitUntil(`document.querySelector('#sandbox').innerText.includes('No extra paths.')`),
	)
	b.run("a path is added read-only, and another read and write",
		isoAdd("/opt/cache", "read"),
		waitUntil(isoPaths+` === '/opt/cache=read'`),
		isoAdd("~/go/pkg/mod", "write"),
		waitUntil(isoPaths+` === '/opt/cache=read,~/go/pkg/mod=write'`),
		waitUntil(`document.querySelector('#sandbox-new-path').value === ''`),
	)
	b.run("access changes on the row",
		chromedp.Evaluate(`(() => { const s = document.querySelector('#sandbox .sandbox-path select'); s.value = 'write'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
		waitUntil(isoPaths+` === '/opt/cache=write,~/go/pkg/mod=write'`),
	)
	b.run("the paths survive a reload",
		chromedp.Reload(),
		waitUntil(isoPaths+` === '/opt/cache=write,~/go/pkg/mod=write'`),
	)
	if got := store.Paths(); len(got) != 2 || !got[0].Write || got[0].Path != "/opt/cache" {
		t.Errorf("stored paths = %+v", got)
	}
	b.run("a path is removed",
		chromedp.Evaluate(isoClick+`('Remove /opt/cache')`, nil),
		waitUntil(isoPaths+` === '~/go/pkg/mod=write'`),
	)

	b.run("a relative path is refused with a message and nothing is added",
		isoAdd("projects/x", "read"),
		waitUntil(`document.querySelector('#sandbox-path-error')?.textContent.includes('Use an absolute path or one starting with ~.')`),
		waitUntil(`document.querySelector('#sandbox-new-path').getAttribute('aria-invalid') === 'true'`),
	)
	if got := store.Paths(); len(got) != 1 {
		t.Errorf("a refused path was stored: %+v", got)
	}
	b.run("an empty path is refused",
		chromedp.Evaluate(`(() => { const i = document.querySelector('#sandbox-new-path'); i.value = ''; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		chromedp.Evaluate(isoClick+`('Add path')`, nil),
		waitUntil(`document.querySelector('#sandbox-path-error')?.textContent.includes('Enter a path.')`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestAPlatformWithoutASandboxDriverIsToldSoOnTheAgentIsolationPage_EndToEnd(t *testing.T) {
	srv, store := agentIsolationServer(t, nil)
	b := newBrowser(t)
	b.run("the page says there is no driver, and the top bar link carries the attention mark",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#sandbox').innerText.includes('No sandbox driver on this platform')`),
		waitUntil(`document.querySelector('header a[aria-label="Agent isolation"]')?.hasAttribute('data-blocking') === true`),
	)
	b.run("with the sandbox off the top bar mark goes away",
		chromedp.Evaluate(`document.querySelector('#sandbox [role=switch]').click()`, nil),
		waitUntil(`document.querySelector('#sandbox [role=alertdialog]')?.checkVisibility() === true`),
		chromedp.Evaluate(isoClick+`('Turn sandbox off')`, nil),
		waitUntil(isoSwitchOff),
	)
	if store.Enabled() {
		t.Error("the sandbox should be off")
	}
	b.run("after a reload the off state shows no blocker",
		chromedp.Reload(),
		waitUntil(isoSwitchOff),
		waitUntil(`!document.querySelector('header a[aria-label="Agent isolation"]').hasAttribute('data-blocking')`),
	)
}
