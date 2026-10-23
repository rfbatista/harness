//go:build !windows

package web_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

const claudePlatform = "linux-x64"

// fakeClaudeReleases is a claude release server whose builds can be held
// halfway, so a test sees the install in progress.
type fakeClaudeReleases struct {
	*httptest.Server
	mu      sync.Mutex
	latest  string
	builds  map[string]string
	hold    chan struct{} // while not nil, the claude file stops halfway until it is closed
	started chan struct{}
}

func newFakeClaudeReleases(t *testing.T, versions ...string) *fakeClaudeReleases {
	t.Helper()
	f := &fakeClaudeReleases{builds: map[string]string{}}
	for _, v := range versions {
		f.add(v)
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeClaudeReleases) add(v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.builds[v] = "#!/bin/sh\necho " + v + "\n" + strings.Repeat("# padding\n", 400)
	f.latest = v
}

func (f *fakeClaudeReleases) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hold, started, latest := f.hold, f.started, f.latest
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var body string
	if len(parts) >= 2 {
		body = f.builds[parts[0]]
	}
	f.mu.Unlock()
	switch {
	case len(parts) == 1 && (parts[0] == "latest" || parts[0] == "stable"):
		fmt.Fprint(w, latest)
	case len(parts) == 2 && parts[1] == "manifest.json" && body != "":
		sum := sha256.Sum256([]byte(body))
		fmt.Fprintf(w, `{"platforms":{%q:{"checksum":%q,"size":%d}}}`, claudePlatform, hex.EncodeToString(sum[:]), len(body))
	case len(parts) == 3 && parts[1] == claudePlatform && parts[2] == "claude" && body != "":
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		if hold == nil {
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, body[:len(body)/2])
		w.(http.Flusher).Flush()
		close(started)
		<-hold
		fmt.Fprint(w, body[len(body)/2:])
	default:
		http.NotFound(w, r)
	}
}

// managedClaudeServer serves the pages and API over a managed claude that
// installs from the fake release server into a temp dir.
func managedClaudeServer(t *testing.T, rel *fakeClaudeReleases, override string) *httptest.Server {
	t.Helper()
	assets := builtAssets(t)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	store := &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"}
	driver := func() ports.SandboxDriver { return &portstest.RecordingSandboxDriver{} }
	svc := managedclaude.New(managedclaude.Deps{Downloads: releases.NewClient(), Dir: filepath.Join(t.TempDir(), "claude"), BaseURL: rel.URL, Platform: claudePlatform})
	t.Cleanup(svc.Close)
	bin := managedclaude.Resolver{Override: override, Managed: svc}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings, SandboxSettings: store, SandboxDriver: driver, ManagedClaude: svc, ManagedClaudeBin: bin})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings, SandboxSettings: store, SandboxDriver: driver, ManagedClaude: svc, ManagedClaudeBin: bin}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const (
	claudeText = `document.querySelector('#claude').innerText`
	// claudeVersions lists the rows as "version" or "version*" when active.
	claudeVersions = `[...document.querySelectorAll('#claude .claude-version')].filter(r => r.checkVisibility()).map(r => r.dataset.version + ([...r.querySelectorAll('.badge')].some(b => b.checkVisibility()) ? '*' : '')).join(',')`
	claudeClick    = `((name) => { const b = [...document.querySelectorAll('#claude button')].find(b => b.checkVisibility() && (b.textContent.trim() === name || b.getAttribute('aria-label') === name)); b.click(); })`
)

// The Managed claude section, end to end: install the first version from a
// fake release server (it becomes active), install another (it does not),
// switch with a separate action (the old one goes), and watch the progress.
func TestInstallingAndSwitchingTheManagedClaudeInTheBrowser(t *testing.T) {
	rel := newFakeClaudeReleases(t, "2.1.47")
	srv := managedClaudeServer(t, rel, "")
	b := newBrowser(t)

	b.run("with nothing installed the section says agents cannot start",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		waitUntil(claudeText+`.includes('No managed claude. Agents cannot start.')`),
		waitUntil(`document.querySelector('#claude .badge').textContent.trim() === 'None'`),
	)
	b.run("Install latest installs it and, with nothing to choose, makes it active",
		chromedp.Evaluate(claudeClick+`('Install latest')`, nil),
		waitUntil(claudeVersions+` === '2.1.47*'`),
		waitUntil(`document.querySelector('#claude .badge').textContent.trim() === '2.1.47'`),
		waitUntil(`!`+claudeText+`.includes('Agents cannot start')`),
	)

	rel.add("2.1.52")
	rel.hold, rel.started = make(chan struct{}), make(chan struct{})
	b.run("installing another version offers it, and shows the download while it runs",
		chromedp.Reload(),
		waitUntil(`[...document.querySelectorAll('#claude select option')].some(o => o.value === '2.1.52' && !o.disabled)`),
		chromedp.Evaluate(claudeClick+`('Install')`, nil),
		waitUntil(claudeText+`.includes('2.1.52 installing')`),
		waitUntil(`/\d+ MB of \d+ MB/.test(`+claudeText+`)`),
		waitUntil(`[...document.querySelectorAll('#claude button')].find(b => b.textContent.trim() === 'Install').disabled`),
	)
	close(rel.hold)
	b.run("the new version is installed beside the active one, which stays active",
		waitUntil(claudeVersions+`.split(',').sort().join(',') === '2.1.47*,2.1.52'`),
		waitUntil(`!`+claudeText+`.includes('installing')`),
	)

	b.run("Use this switches, says so, and the old version is gone",
		chromedp.Evaluate(claudeClick+`('Use 2.1.52')`, nil),
		waitUntil(claudeVersions+` === '2.1.52*'`),
		waitUntil(`document.querySelector('#claude .badge').textContent.trim() === '2.1.52'`),
		waitUntil(claudeText+`.includes('New sessions will use 2.1.52.')`),
	)
	b.run("the switch holds across a reload",
		chromedp.Reload(),
		waitUntil(claudeVersions+` === '2.1.52*'`),
		waitUntil(`document.querySelector('#claude .badge').textContent.trim() === '2.1.52'`),
	)
}

// A failed install says which version and why, and installs nothing.
func TestAFailedManagedClaudeInstallIsReportedInTheBrowser(t *testing.T) {
	rel := newFakeClaudeReleases(t)
	rel.latest = "9.9.9" // named, but with no build behind it
	srv := managedClaudeServer(t, rel, "")
	b := newBrowser(t)
	b.run("the failure names the version and the reason, and the list stays empty",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		chromedp.Evaluate(claudeClick+`('Install latest')`, nil),
		waitUntil(claudeText+`.includes('Could not install 9.9.9.') && `+claudeText+`.includes('does not exist')`),
		waitUntil(claudeVersions+` === ''`),
		waitUntil(`document.querySelector('#claude .badge').textContent.trim() === 'None'`),
	)
	b.run("it is still there after a reload, with Retry",
		chromedp.Reload(),
		waitUntil(claudeText+`.includes('Could not install 9.9.9.')`),
		waitUntil(`[...document.querySelectorAll('#claude button')].some(b => b.checkVisibility() && b.textContent.trim() === 'Retry')`),
	)
}

// The -claude.bin override is named on the page.
func TestTheManagedClaudeSectionNamesTheClaudeBinOverride(t *testing.T) {
	srv := managedClaudeServer(t, newFakeClaudeReleases(t), "/opt/dev/claude")
	b := newBrowser(t)
	b.run("the override banner",
		chromedp.Navigate(srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		waitUntil(claudeText+`.includes('Overridden by -claude.bin.') && `+claudeText+`.includes('Agents run /opt/dev/claude.')`),
		waitUntil(`!`+claudeText+`.includes('Agents cannot start')`),
	)
}
