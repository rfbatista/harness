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
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/domain"
)

// The "Agents move documents to project" switch on the Server page, end to
// end: the person finds it off, turns it on and sees it saved after a
// reload, then turns it off again.
func TestAgentsMoveDocumentsIsSwitchedOnTheServerPage_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects:  cat.Projects,
		Settings:  cat.Settings,
		DataPaths: &serversettings.Paths{DataDir: t.TempDir(), Source: "a test", Database: ":memory:"},
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const sw = `document.querySelector('input[data-setting="` + domain.SettingAgentsMoveDocuments + `"]')`
	b := newBrowser(t)
	b.run("the switch is labelled and off while the setting is unset",
		chromedp.Navigate(srv.URL+"/server"),
		waitUntil(sw+` !== null && `+sw+`.checked === false`),
		waitUntil(`document.body.textContent.includes('Agents move documents to project')`),
	)
	b.run("turn it on",
		chromedp.Evaluate(sw+`.click()`, nil),
		waitUntil(`[...document.querySelectorAll('main [aria-live]')].some(e => e.textContent.includes('saved'))`),
	)
	if got := cat.Settings.Setting(domain.SettingAgentsMoveDocuments); got != "true" {
		t.Errorf("stored setting = %q, want true", got)
	}
	b.run("it is still on after a reload",
		chromedp.Reload(),
		waitUntil(sw+` !== null && `+sw+`.checked === true`),
	)
	b.run("turn it off",
		chromedp.Evaluate(sw+`.click()`, nil),
		waitUntil(`[...document.querySelectorAll('main [aria-live]')].some(e => e.textContent.includes('saved'))`),
		chromedp.Reload(),
		waitUntil(sw+` !== null && `+sw+`.checked === false`),
	)
	if got := cat.Settings.Setting(domain.SettingAgentsMoveDocuments); got != "false" {
		t.Errorf("stored setting = %q, want false", got)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
