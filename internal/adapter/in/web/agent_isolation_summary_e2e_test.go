//go:build !windows

package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// isolationWorld is the whole Agent isolation page over real services: the
// agent credential in SQLite, a managed claude installing from a fake release
// server, the sandbox settings over a real home directory, and a built-in
// allowlist from a real policy builder.
type isolationWorld struct {
	srv     *httptest.Server
	rel     *fakeClaudeReleases
	claude  *managedclaude.Service
	sandbox *sandboxsettings.Store
	home    string
	running atomic.Int32
}

func newIsolationWorld(t *testing.T, driver bool, versions ...string) *isolationWorld {
	t.Helper()
	w := &isolationWorld{rel: newFakeClaudeReleases(t, versions...), home: t.TempDir()}
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
	w.sandbox = &sandboxsettings.Store{Settings: cat.Settings, Home: w.home}
	current := func() ports.SandboxDriver { return nil }
	if driver {
		current = func() ports.SandboxDriver { return &portstest.RecordingSandboxDriver{} }
	}
	w.claude = managedclaude.New(managedclaude.Deps{
		Downloads: releases.NewClient(), Dir: filepath.Join(dir, "claude"), BaseURL: w.rel.URL, Platform: claudePlatform,
		RunningSessions: func() int { return int(w.running.Load()) },
	})
	t.Cleanup(w.claude.Close)
	bin := managedclaude.Resolver{Managed: w.claude}
	builder := &orchestration.Sandbox{Home: "/data/agent-home", Allow: func(string) []ports.SandboxPath {
		return []ports.SandboxPath{{Path: "/home/ana/go", Write: true}, {Path: "/home/ana/.rustup"}, {Path: "/data/tools"}}
	}}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Settings: cat.Settings, SandboxSettings: w.sandbox, SandboxDriver: current,
		ManagedClaude: w.claude, ManagedClaudeBin: bin, AgentCredential: cred})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings, SandboxSettings: w.sandbox, SandboxDriver: current,
		ManagedClaude: w.claude, ManagedClaudeBin: bin, AgentCredential: cred, SandboxBuiltin: builder.Builtin}, assets, nil))
	w.srv = httptest.NewServer(mux)
	t.Cleanup(w.srv.Close)
	return w
}

// setCredential saves an agent credential through the API, as an action.
func (w *isolationWorld) setCredential(t *testing.T) chromedp.Action {
	return chromedp.ActionFunc(func(context.Context) error {
		res, err := http.Post(w.srv.URL+"/api/set_agent_credential", "application/json", strings.NewReader(`{"kind":"oauth","value":"sk-ant-oat01-x"}`))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("set credential = %d", res.StatusCode)
		}
		return nil
	})
}

const (
	isoSwitchLabel = "Run agents in the sandbox"
	isoChips       = `[...document.querySelectorAll('nav[aria-label=Summary] a')].map(a => a.textContent.trim() + '|' + a.dataset.tone).join(';')`
	// isoBlockedSentence is whether "Agents cannot start until this is fixed." shows.
	isoBlockedSentence = `[...document.querySelectorAll('nav[aria-label=Summary] span')].some(s => s.checkVisibility() && s.textContent.includes('Agents cannot start until this is fixed.'))`
	isoMark            = `document.querySelector('header a[aria-label="Agent isolation"]')?.hasAttribute('data-blocking')`
	claudeBtn          = `((name) => [...document.querySelectorAll('#claude button')].find(b => b.checkVisibility() && (b.textContent.trim() === name || b.getAttribute('aria-label') === name)))`
)

func TestTheSummaryChipsShowTheTrueStateAndEachJumpsToItsSection(t *testing.T) {
	w := newIsolationWorld(t, true, "2.1.47")
	b := newBrowser(t)

	b.run("with nothing set the chips say what blocks agents",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		waitUntil(isoChips+` === 'Credential: missing|danger;claude: not installed|danger;Sandbox: on|signal'`),
		waitUntil(isoBlockedSentence),
		waitUntil(isoMark),
	)
	for _, c := range []struct{ chip, hash string }{{"credential", "#credential"}, {"claude", "#claude"}, {"sandbox", "#sandbox"}} {
		b.run(c.chip+" chip jumps to its section",
			chromedp.Evaluate(`document.querySelector('nav[aria-label=Summary] a[data-chip=`+c.chip+`]').click()`, nil),
			waitUntil(`location.hash === `+quote(c.hash)),
			waitUntil(`document.querySelector(`+quote(c.hash)+`) !== null`),
		)
	}

	b.run("installing claude updates its chip at once, without a reload",
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install latest'))`, nil),
		waitUntil(isoChips+`.includes('claude: 2.1.47|signal')`),
		waitUntil(isoBlockedSentence),
	)
	b.run("saving a credential clears the last blocker",
		w.setCredential(t),
		chromedp.Reload(),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		waitUntil(isoChips+` === 'Credential: set|signal;claude: 2.1.47|signal;Sandbox: on|signal'`),
		waitUntil(`!(`+isoBlockedSentence+`)`),
		waitUntil(`!`+isoMark),
	)
	b.run("turning the sandbox off updates its chip",
		chromedp.Evaluate(isoClick+`('Run agents in the sandbox')`, nil),
		waitUntil(`document.querySelector('#sandbox [role=alertdialog]')?.checkVisibility() === true`),
		chromedp.Evaluate(isoClick+`('Turn sandbox off')`, nil),
		waitUntil(isoChips+`.includes('Sandbox: off|attention')`),
	)
}

func TestTheSummaryChipNamesAPlatformWithoutADriver(t *testing.T) {
	w := newIsolationWorld(t, false)
	b := newBrowser(t)
	b.run("no driver blocks",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		waitUntil(isoChips+`.includes('Sandbox: no driver|danger')`),
	)
}

func TestAnInstallThatRunsCanBeCancelledAndLeavesNothingBehind(t *testing.T) {
	w := newIsolationWorld(t, true, "2.1.47")
	w.rel.hold, w.rel.started = make(chan struct{}), make(chan struct{})
	hold := w.rel.hold
	t.Cleanup(func() { close(hold) })
	b := newBrowser(t)
	b.run("an install that runs offers Cancel",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install latest'))`, nil),
		waitUntil(claudeText+`.includes('2.1.47 installing')`),
		waitUntil(`!!`+claudeBtn+`('Cancel install of 2.1.47')`),
	)
	b.run("Cancel ends it with no failure, no version and no install row",
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Cancel install of 2.1.47'))`, nil),
		waitUntil(`!`+claudeText+`.includes('installing')`),
		waitUntil(claudeText+`.includes('Install of 2.1.47 cancelled.')`),
		waitUntil(`!`+claudeText+`.includes('Could not install')`),
		waitUntil(claudeVersions+` === ''`),
		waitUntil(`document.querySelector('#claude .badge').textContent.trim() === 'None'`),
		waitUntil(`!!`+claudeBtn+`('Install latest')`),
	)
	if st := w.claude.State(); st.Installing != nil || st.Failed != nil || len(st.Installed) != 0 {
		t.Errorf("state after Cancel = %+v", st)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(w.claude.Path()), "versions", "*"))
	if len(left) != 0 {
		t.Errorf("the cancelled install left %v", left)
	}
	w.rel.mu.Lock()
	w.rel.hold = nil
	w.rel.mu.Unlock()
	b.run("the install can be started again after a Cancel",
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install latest'))`, nil),
		waitUntil(claudeVersions+` === '2.1.47*'`),
	)
}

func TestANonCurrentVersionCanBeRemovedAndTheCurrentOneCannot(t *testing.T) {
	w := newIsolationWorld(t, true, "2.1.47")
	b := newBrowser(t)
	b.run("install the first version, then a second beside it",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install latest'))`, nil),
		waitUntil(claudeVersions+` === '2.1.47*'`),
	)
	w.rel.add("2.1.52")
	b.run("the second version is installed",
		chromedp.Reload(),
		waitUntil(`[...document.querySelectorAll('#claude select option')].some(o => o.value === '2.1.52' && !o.disabled)`),
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install'))`, nil),
		waitUntil(claudeVersions+`.split(',').sort().join(',') === '2.1.47*,2.1.52'`),
	)
	b.run("only the version that is not current offers Remove",
		waitUntil(`!`+claudeBtn+`('Remove 2.1.47')`),
		waitUntil(`!!`+claudeBtn+`('Remove 2.1.52')`),
	)
	b.run("removing it leaves the current version alone",
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Remove 2.1.52'))`, nil),
		waitUntil(claudeVersions+` === '2.1.47*'`),
		waitUntil(claudeText+`.includes('Removed 2.1.52.')`),
		waitUntil(`!`+claudeBtn+`('Remove 2.1.47')`),
	)
	if _, err := os.Stat(filepath.Join(filepath.Dir(w.claude.Path()), "versions", "2.1.52")); !os.IsNotExist(err) {
		t.Errorf("the removed version's files remain: %v", err)
	}
	// The server refuses the current one too, whatever a page asks.
	if _, err := w.claude.Remove("2.1.47"); err == nil {
		t.Error("the current version was removed")
	}
}

func TestAfterASwitchTheStatusLineNamesTheRunningSessionsThatKeepTheirVersion(t *testing.T) {
	w := newIsolationWorld(t, true, "2.1.47")
	w.running.Store(2)
	b := newBrowser(t)
	b.run("install two versions",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install latest'))`, nil),
		waitUntil(claudeVersions+` === '2.1.47*'`),
	)
	w.rel.add("2.1.52")
	b.run("the switch says who keeps what",
		chromedp.Reload(),
		waitUntil(`[...document.querySelectorAll('#claude select option')].some(o => o.value === '2.1.52' && !o.disabled)`),
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install'))`, nil),
		waitUntil(claudeVersions+`.split(',').sort().join(',') === '2.1.47*,2.1.52'`),
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Use 2.1.52'))`, nil),
		waitUntil(`document.querySelector('#claude [data-switch-line]').checkVisibility() && document.querySelector('#claude [data-switch-line]').textContent.trim() === 'New sessions will use 2.1.52. 2 running sessions keep 2.1.47.'`),
	)
}

func TestTheTopBarMarksNoManagedClaudeUntilOneIsInstalled(t *testing.T) {
	w := newIsolationWorld(t, true, "2.1.47")
	b := newBrowser(t)
	b.run("with the credential set, no managed claude alone carries the mark",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		w.setCredential(t),
		waitUntil(`document.querySelector('#claude [data-ssr]') === null`),
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(isoChips+`.startsWith('Credential: set|signal;claude: not installed|danger')`),
		waitUntil(isoMark),
	)
	b.run("once one is installed the mark is gone",
		chromedp.Evaluate(`((b) => b.click())(`+claudeBtn+`('Install latest'))`, nil),
		waitUntil(claudeVersions+` === '2.1.47*'`),
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('header a[aria-label="Agent isolation"]') !== null`),
		waitUntil(`!`+isoMark),
	)
}

func TestAPathThatDoesNotExistYetOrHoldsCredentialsIsWarnedAboutAndStillSaved(t *testing.T) {
	w := newIsolationWorld(t, true)
	if err := os.MkdirAll(filepath.Join(w.home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(w.home, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t)
	rowText := func(path string) string {
		return `((r) => r ? r.innerText : '')([...document.querySelectorAll('#sandbox .sandbox-path')].find(r => r.dataset.path === ` + quote(path) + `))`
	}
	b.run("a folder that exists has no warning",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#sandbox [data-ssr]') === null`),
		isoAdd("~/assets", "read"),
		waitUntil(isoPaths+` === '~/assets=read'`),
		waitUntil(`!`+rowText("~/assets")+`.includes('does not exist') && !`+rowText("~/assets")+`.includes('credentials')`),
	)
	b.run("a folder that does not exist yet warns, and is saved",
		isoAdd("~/later", "write"),
		waitUntil(isoPaths+`.includes('~/later=write')`),
		waitUntil(rowText("~/later")+`.includes('does not exist yet')`),
	)
	b.run("a credential folder warns, and is saved",
		isoAdd("~/.ssh", "read"),
		waitUntil(isoPaths+`.includes('~/.ssh=read')`),
		waitUntil(rowText("~/.ssh")+`.includes('usually holds credentials')`),
		waitUntil(`!`+rowText("~/.ssh")+`.includes('does not exist')`),
	)
	b.run("a missing credential folder shows both",
		isoAdd("~/.aws", "read"),
		waitUntil(rowText("~/.aws")+`.includes('does not exist yet') && `+rowText("~/.aws")+`.includes('usually holds credentials')`),
	)
	if got := len(w.sandbox.Paths()); got != 4 {
		t.Errorf("saved %d paths, want 4: %v", got, w.sandbox.Paths())
	}
	b.run("the warnings are still there after a reload",
		chromedp.Reload(),
		waitUntil(`document.querySelector('#sandbox [data-ssr]') === null`),
		waitUntil(rowText("~/.aws")+`.includes('does not exist yet') && `+rowText("~/.aws")+`.includes('usually holds credentials')`),
		waitUntil(rowText("~/later")+`.includes('does not exist yet')`),
	)
}

func TestTheBuiltinAllowlistIsAClosedListOfWhatTheBuilderGives(t *testing.T) {
	w := newIsolationWorld(t, true)
	b := newBrowser(t)
	var summary string
	b.run("the list is closed and says how many paths it holds",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#sandbox [data-ssr]') === null`),
		waitUntil(`document.querySelector('#sandbox-builtin') !== null && document.querySelector('#sandbox-builtin').open === false`),
		chromedp.Evaluate(`document.querySelector('#sandbox-builtin summary').textContent.trim()`, &summary),
	)
	if summary != "Built-in allowlist (17 paths, not editable)" {
		t.Errorf("summary = %q", summary)
	}
	var text string
	b.run("opened, it lists the agent home, shell, temp dir, repositories by mode and the standing paths",
		chromedp.Evaluate(`document.querySelector('#sandbox-builtin summary').click()`, nil),
		waitUntil(`document.querySelector('#sandbox-builtin').open`),
		chromedp.Evaluate(`document.querySelector('#sandbox-builtin').innerText`, &text),
	)
	for _, want := range []string{
		"/data/agent-home", "Agent Claude home", "virtual shell", "Temp dir",
		"Architect sessions: every repository of the project", "Product sessions: every repository of the project",
		"/home/ana/go", "/home/ana/.rustup", "/data/tools", "Read and write", "Read-only",
	} {
		if !containsFold(text, want) {
			t.Errorf("the built-in list lacks %q:\n%s", want, text)
		}
	}
}

func TestAllowPathFromTheSandboxTabOpensTheAddFieldPrefilledAndSavesNothing(t *testing.T) {
	w := newIsolationWorld(t, true)
	b := newBrowser(t)
	path := "/Users/me/.cache/pnpm/store/tmp"
	var typed string
	b.run("the field holds the path, with a note that it is not saved",
		chromedp.Navigate(w.srv.URL+"/agent-isolation?allow_path="+url.QueryEscape(path)+"#sandbox"),
		waitUntil(`document.querySelector('#sandbox [data-ssr]') === null`),
		waitUntil(`document.querySelector('#sandbox-new-path').value === `+quote(path)),
		waitUntil(`document.querySelector('#sandbox [data-prefilled]').checkVisibility()`),
		waitUntil(`document.activeElement === document.querySelector('#sandbox-new-path')`),
		chromedp.Evaluate(`document.querySelector('#sandbox-new-path').value`, &typed),
	)
	if got := w.sandbox.Paths(); len(got) != 0 {
		t.Fatalf("following the link saved %v", got)
	}
	b.run("Add path saves it and clears the note",
		chromedp.Evaluate(isoClick+`('Add path')`, nil),
		waitUntil(isoPaths+` === `+quote(path+"=read")),
		waitUntil(`!document.querySelector('#sandbox [data-prefilled]').checkVisibility()`),
		waitUntil(`document.querySelector('#sandbox-new-path').value === ''`),
	)
	if got := w.sandbox.Paths(); len(got) != 1 || got[0].Path != path {
		t.Errorf("saved %v", got)
	}
	b.run("a page without the link has an empty field and no note",
		chromedp.Navigate(w.srv.URL+"/agent-isolation"),
		waitUntil(`document.querySelector('#sandbox [data-ssr]') === null`),
		waitUntil(`document.querySelector('#sandbox-new-path').value === '' && !document.querySelector('#sandbox [data-prefilled]').checkVisibility()`),
	)
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
