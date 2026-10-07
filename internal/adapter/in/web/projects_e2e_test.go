//go:build !windows

package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Project management in the web UI, end to end: the server as it runs (the
// projects context with its summaries and catalog feed, planning,
// orchestration, the HTTP API and the pages) on one test server, and two
// people, each in their own headless Chrome at phone width: one on the
// projects list, one on a project's settings. A rename on one shows on the
// other without a reload; a delete is refused while a session runs, naming
// it, and goes through once it ends. Screenshots land in PROJECT_SHOTS when
// it names a directory.
func TestProjectManagementInTheWebUI_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx := context.Background()
	harnessDir, poolDir := t.TempDir(), t.TempDir()
	var (
		cat      catalog.Catalog
		orch     *orchestration.Service
		plan     *planning.Service
		sessions ports.SessionRepository
	)
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: harnessDir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&cat, &orch, &plan, &sessions),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })

	harness, err := cat.Projects.CreateProject(ctx, "harness", harnessDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Projects.CreateProject(ctx, "coding_pool", poolDir); err != nil {
		t.Fatal(err)
	}
	task, err := plan.CreateTicket(ctx, harness.ID, "Project management", "", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(&domain.Session{ID: "s-running-1", ProjectID: harness.ID, TicketID: task.ID, Task: "web", WorkingDir: t.TempDir(), Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, ProjectSummaries: cat.Projects,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the pages' event streams never end on their own
		srv.Close()
	})

	lister, admin := newBrowser(t), newBrowser(t)
	const row = `document.querySelector('main[x-data="projectsListPage"] [data-project-id="` + "%s" + `"]')`
	rowNames := `[...document.querySelectorAll('main[x-data="projectsListPage"] section .row a.title')].map(a => a.textContent).join(',')`
	noSideScroll := `document.documentElement.scrollWidth <= window.innerWidth`

	lister.run("open the projects list at phone width",
		chromedp.EmulateViewport(375, 812),
		chromedp.Navigate(srv.URL+"/projects"),
		waitUntil(rowNames+` === 'coding_pool,harness'`),
		waitUntil(`document.querySelector('.stream-bar [data-state="live"]') !== null`),
		waitUntil(strings.Replace(row, "%s", harness.ID, 1)+`.textContent.includes('1 running')`),
		waitUntil(noSideScroll),
		bothSchemes("projects-list"),
	)

	// By keyboard: j moves to harness, s opens its settings.
	lister.run("j then s opens the selected project's settings",
		chromedp.KeyEvent("j"),
		waitUntil(strings.Replace(row, "%s", harness.ID, 1)+`.getAttribute('aria-selected') === 'true'`),
		chromedp.KeyEvent("s"),
		atPath(srv.URL+"/projects/"+harness.ID+"/settings"),
		chromedp.WaitVisible(`main[x-data="projectsSettingsPage"] #project-name`, chromedp.ByQuery),
	)
	lister.run("back to the list",
		chromedp.Navigate(srv.URL+"/projects"),
		waitUntil(rowNames+` === 'coding_pool,harness'`),
		waitUntil(`document.querySelector('.stream-bar [data-state="live"]') !== null`),
	)

	const settings = `document.querySelector('main[x-data="projectsSettingsPage"]')`
	admin.run("open harness's settings at phone width",
		chromedp.EmulateViewport(375, 812),
		chromedp.Navigate(srv.URL+"/projects/"+harness.ID+"/settings"),
		waitUntil(settings+` && !document.querySelector('[data-ssr]') && document.querySelector('#project-name').value === 'harness'`),
		waitUntil(noSideScroll),
		bothSchemes("project-settings"),
	)

	admin.run("a taken name is refused under the field, with its code",
		setField("#project-name", "Coding_Pool"),
		clickButton(settings, "Save changes"),
		waitUntil(`document.querySelector('#project-name-error')?.textContent.includes('PROJECT_NAME_TAKEN')`),
		waitUntil(`document.querySelector('#project-name').getAttribute('aria-invalid') === 'true'`),
		bothSchemes("project-settings-name-taken"),
	)

	admin.run("rename it; the other person's list follows without a reload",
		setField("#project-name", "harness-ui"),
		clickButton(settings, "Save changes"),
		waitUntil(settings+`.textContent.includes('Saved.')`),
	)
	lister.run("the rename arrives on the list",
		waitUntil(rowNames+` === 'coding_pool,harness-ui'`),
	)

	admin.run("delete is refused while a session runs, naming it with a link to its task",
		clickButton(settings, "Delete project…"),
		waitUntil(`document.querySelector('#confirm-name') !== null`),
		setField("#confirm-name", "harness-ui"),
		waitUntil(`!document.querySelector('section[data-tone="danger"] button[type=submit]').disabled`),
		chromedp.Evaluate(`document.querySelector('section[data-tone="danger"] button[type=submit]').click()`, nil),
		waitUntil(`document.querySelector('section[data-tone="danger"] [role=alert]')?.textContent.includes('1 running session')`),
		waitUntil(`document.querySelector('section[data-tone="danger"] [role=alert] a[href="/projects/`+harness.ID+`/tasks/`+task.ID+`"]') !== null`),
		waitUntil(noSideScroll),
		bothSchemes("project-delete-refused"),
	)
	var banner string
	admin.run("read the refusal", chromedp.Evaluate(`document.querySelector('section[data-tone="danger"] [role=alert]').textContent`, &banner))
	if strings.Contains(banner, "PROJECT_HAS_RUNNING_SESSIONS:") {
		t.Errorf("the banner shows the server's raw message: %q", banner)
	}
	if _, err := cat.Projects.GetProject(ctx, harness.ID); err != nil {
		t.Fatalf("the refused project is gone: %v", err)
	}

	if err := sessions.UpdateStatus("s-running-1", domain.SessionDone); err != nil {
		t.Fatal(err)
	}
	admin.run("once the session ends, the delete goes through and leaves for the list",
		chromedp.Evaluate(`document.querySelector('section[data-tone="danger"] button[type=submit]').click()`, nil),
		atPath(srv.URL+"/projects"),
		waitUntil(rowNames+` === 'coding_pool'`),
	)
	lister.run("the deleted project leaves the other person's list too",
		waitUntil(rowNames+` === 'coding_pool'`),
		waitUntil(`document.querySelector('main[x-data="projectsListPage"]').textContent.includes('1 project')`),
	)
	if _, err := cat.Projects.GetProject(ctx, harness.ID); err == nil {
		t.Error("the project is still there after the delete")
	}
	for _, b := range []*browser{lister, admin} {
		if errs := b.jsErrors(); len(errs) > 0 {
			t.Errorf("JS errors: %v", errs)
		}
	}
}

// browser is one person's headless Chrome.
type browser struct {
	t      *testing.T
	ctx    context.Context
	mu     sync.Mutex
	errors []string
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	ctx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-extensions", true))...)
	ctx, cancelBrowser := chromedp.NewContext(ctx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(func() { cancelTimeout(); cancelBrowser(); cancelAlloc() })
	b := &browser{t: t, ctx: ctx}
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*runtime.EventExceptionThrown); ok {
			b.mu.Lock()
			b.errors = append(b.errors, e.ExceptionDetails.Error())
			b.mu.Unlock()
		}
	})
	if err := chromedp.Run(ctx); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) || strings.Contains(err.Error(), "executable file not found") {
			t.Skipf("browser test: Chrome not found (%v)", err)
		}
		t.Fatal(err)
	}
	return b
}

func (b *browser) jsErrors() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.errors...)
}

// run runs one step, given 30s, and names it when it fails.
func (b *browser) run(step string, actions ...chromedp.Action) {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, actions...); err != nil {
		var page string
		_ = chromedp.Run(b.ctx, chromedp.Evaluate(`document.querySelector('main')?.innerText.slice(0, 3000) ?? ''`, &page))
		b.t.Fatalf("%s: %v\npage:\n%s\nJS errors: %v", step, err, page, b.jsErrors())
	}
}

// waitUntil polls js every 100ms for up to 10s (not on animation frames: a
// background browser gets none).
func waitUntil(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingInterval(100*time.Millisecond), chromedp.WithPollingTimeout(10*time.Second))
}

// atPath waits until the tab is at url, riding out the navigation that gets
// it there (a poll inside the page would die with the old document).
func atPath(url string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var got string
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if err := chromedp.Location(&got).Do(ctx); err == nil && got == url {
				return nil
			}
		}
		return errors.New("still at " + got + ", want " + url)
	})
}

// setField types value into the field as a person would, for x-model.
func setField(selector, value string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const el = document.querySelector(`+quote(selector)+`);
		el.focus(); el.value = `+quote(value)+`; el.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}

// clickButton clicks the enabled button inside root (a JS expression) whose text is text.
func clickButton(root, text string) chromedp.Action {
	return waitUntil(`(() => { const b = [...(` + root + `?.querySelectorAll('button') ?? [])].find(b => b.textContent.trim() === ` + quote(text) + ` && !b.disabled);
		if (!b) return false; b.click(); return true; })()`)
}

func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// bothSchemes saves name-light.png and name-dark.png in PROJECT_SHOTS; without it, nothing.
func bothSchemes(name string) chromedp.Action {
	dir := os.Getenv("PROJECT_SHOTS")
	if dir == "" {
		return chromedp.ActionFunc(func(context.Context) error { return nil })
	}
	var tasks chromedp.Tasks
	for _, scheme := range []string{"light", "dark"} {
		var buf []byte
		tasks = append(tasks,
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}}),
			chromedp.Sleep(150*time.Millisecond), // the theme's transition
			chromedp.FullScreenshot(&buf, 90),
			chromedp.ActionFunc(func(context.Context) error {
				return os.WriteFile(filepath.Join(dir, name+"-"+scheme+".png"), buf, 0o644)
			}),
		)
	}
	return tasks
}
