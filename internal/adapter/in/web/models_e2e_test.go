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
	"operators-mcp/internal/domain"
)

// The Models page, end to end: the settings context in SQLite, the HTTP API
// and the pages, and a person in headless Chrome. They find the page linked from
// the top bar, see the built-in models, move implementation sessions to
// haiku, and find it stored and still chosen after a reload.
func TestSessionModelsAreSetOnTheModelsPage_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const (
		architect      = `(document.querySelector('#model-Architect')?.value ?? '')`
		implementation = `(document.querySelector('#model-Implementation')?.value ?? '')`
	)
	b := newBrowser(t)
	b.run("the top bar links to the page",
		chromedp.Navigate(srv.URL+"/projects/new"),
		waitUntil(`document.querySelector('header a[aria-label="Models"]')?.getAttribute('href') === '/models'`),
	)
	b.run("the tiers show the built-in models",
		chromedp.Navigate(srv.URL+"/models"),
		waitUntil(architect+` === 'opus'`),
		waitUntil(implementation+` === 'sonnet'`),
	)
	b.run("move implementation sessions to haiku",
		chromedp.Evaluate(`(() => { const s = document.querySelector('#model-Implementation');
			s.value = 'haiku'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
		waitUntil(`[...document.querySelectorAll('main [aria-live]')].some(e => e.textContent.includes('Model set to haiku'))`),
	)
	b.run("haiku stays across a reload",
		chromedp.Reload(),
		waitUntil(implementation+` === 'haiku'`),
		waitUntil(architect+` === 'opus'`),
	)
	if got := cat.Settings.Setting(domain.SettingModelImplementation); got != "haiku" {
		t.Errorf("stored implementation model = %q, want haiku", got)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
