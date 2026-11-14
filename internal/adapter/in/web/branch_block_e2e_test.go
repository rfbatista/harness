//go:build !windows

package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/domain"
)

// The "Branch block" section of the Server page, end to end: six switches,
// all on while nothing is stored; the person turns Implement off, the setting
// is stored as off, and a reload shows it still off.
func TestBranchBlockIsSwitchedPerModeOnTheServerPage_EndToEnd(t *testing.T) {
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

	const all = `[...document.querySelectorAll('input[data-branch-block]')]`
	implement := `document.querySelector('input[data-setting="` + domain.BranchBlockKey(domain.SessionModeImplement) + `"]')`
	b := newBrowser(t)
	b.run("six switches, all on while nothing is stored",
		chromedp.Navigate(srv.URL+"/server"),
		waitUntil(all+`.length === 6 && `+all+`.every(e => e.checked)`),
		waitUntil(`document.body.textContent.includes('Branch block')`),
		waitUntil(`document.body.textContent.includes('stay on their session branch')`),
		waitUntil(all+`.map(e => e.dataset.setting).join() === `+jsList(branchBlockKeys())),
	)
	b.run("turn Implement off",
		chromedp.Evaluate(implement+`.click()`, nil),
		waitUntil(`[...document.querySelectorAll('main [aria-live]')].some(e => e.textContent.includes('saved'))`),
	)
	if got := cat.Settings.Setting(domain.BranchBlockKey(domain.SessionModeImplement)); got != "off" {
		t.Errorf("stored Implement setting = %q, want off", got)
	}
	for _, m := range []domain.SessionMode{domain.SessionModeDefault, domain.SessionModeArchitect, domain.SessionModeDesign, domain.SessionModeProduct, domain.SessionModeDesignSystem} {
		if got := cat.Settings.Setting(domain.BranchBlockKey(m)); got != "" {
			t.Errorf("stored %s setting = %q, want unset", m, got)
		}
	}
	b.run("after a reload Implement is still off and the rest on",
		chromedp.Reload(),
		waitUntil(all+`.length === 6`),
		waitUntil(implement+`.checked === false && `+all+`.filter(e => e.checked).length === 5`),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func branchBlockKeys() []string {
	var keys []string
	for _, bb := range domain.BranchBlocks(func(string) string { return "" }) {
		keys = append(keys, bb.Key)
	}
	return keys
}

func jsList(items []string) string { return "'" + strings.Join(items, ",") + "'" }

// A save the server refuses shows its message in the page's banner and puts
// the switch back; every switch is named by its mode and "Branch block", and
// Space on a focused one toggles and saves it.
func TestARefusedBranchBlockSaveRestoresTheSwitch_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	api := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings}))
	var refuse atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/update_settings", func(w http.ResponseWriter, r *http.Request) {
		if refuse.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "INVALID_INPUT", "error": "The branch block was not saved."})
			return
		}
		api.ServeHTTP(w, r)
	})
	mux.Handle("/api/", api)
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects:  cat.Projects,
		Settings:  cat.Settings,
		DataPaths: &serversettings.Paths{DataDir: t.TempDir(), Source: "a test", Database: ":memory:"},
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const all = `[...document.querySelectorAll('input[data-branch-block]')]`
	// The page has other alerts (Services); this is the Branch block section's.
	const alert = `'section[aria-labelledby="server-branch-block-title"] [role="alert"]'`
	design := `document.querySelector('input[data-setting="` + domain.BranchBlockKey(domain.SessionModeDesign) + `"]')`
	implement := `document.querySelector('input[data-setting="` + domain.BranchBlockKey(domain.SessionModeImplement) + `"]')`
	b := newBrowser(t)
	b.run("each switch is named by its mode and Branch block",
		chromedp.Navigate(srv.URL+"/server"),
		waitUntil(all+`.length === 6`),
		waitUntil(all+`.map(e => e.getAttribute('aria-label')).join() === 'Default Branch block,Architect Branch block,Implement Branch block,Design Branch block,Product Branch block,Design system Branch block'`),
	)
	refuse.Store(true)
	b.run("a refused save shows the server's message and restores the switch",
		chromedp.Evaluate(design+`.click()`, nil),
		waitUntil(`document.querySelector(`+alert+`)?.textContent.includes('The branch block was not saved.') === true`),
		waitUntil(design+`.checked === true && !`+design+`.disabled`),
		chromedp.Evaluate(`document.querySelector(`+alert+`+' button').click()`, nil),
		waitUntil(`document.querySelector(`+alert+`) === null`),
	)
	if got := cat.Settings.Setting(domain.BranchBlockKey(domain.SessionModeDesign)); got != "" {
		t.Errorf("stored Design setting = %q, want unset", got)
	}
	refuse.Store(false)
	b.run("Space on the focused Implement switch toggles and saves it",
		chromedp.Evaluate(implement+`.focus()`, nil),
		chromedp.KeyEvent(" "),
		waitUntil(`[...document.querySelectorAll('main [aria-live]')].some(e => e.textContent.includes('saved'))`),
	)
	if got := cat.Settings.Setting(domain.BranchBlockKey(domain.SessionModeImplement)); got != "off" {
		t.Errorf("stored Implement setting = %q, want off", got)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
