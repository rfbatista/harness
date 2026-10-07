//go:build !windows

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Project design assets attached to tasks, in the web UI, end to end: the
// server as it runs (artifacts, planning, the project feed, the HTTP API and
// the pages) on one test server, and a headless Chrome as the person, in two
// tabs: the project's design library and a task's design assets page. What
// one tab does shows on the other without a reload. Screenshots land in
// ARCHITECT_SHOTS when it names a directory.
func TestDesignAssetsAttachedToTasksInTheWebUI_EndToEnd(t *testing.T) {
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
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		orch     *orchestration.Service
		plan     *planning.Service
		art      *artifacts.Service
		sessions ports.SessionRepository
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&cat, &orch, &plan, &art, &sessions),
		fx.NopLogger,
	)
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	p, err := cat.Projects.CreateProject(ctx, "harness", dir)
	if err != nil {
		t.Fatal(err)
	}
	brand, _ := plan.CreateTicket(ctx, p.ID, "Brand refresh", "", domain.TicketStatusInProgress)
	checkout, _ := plan.CreateTicket(ctx, p.ID, "Checkout", "", domain.TicketStatusTodo)
	if _, err := plan.CreateTicket(ctx, p.ID, "Landing", "", domain.TicketStatusTodo); err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	if _, err := sessions.Create(&domain.Session{ID: "s-design", ProjectID: p.ID, TicketID: brand.ID, Task: "logo", WorkingDir: worktree, Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "logo.html"), []byte("<p>logo</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	logo, err := art.Publish(ctx, ports.PublishArtifactRequest{SessionID: "s-design", Path: "logo.html", Title: "Logo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := art.SetArtifactScope(ctx, logo.ID, domain.ArtifactScopeProject); err != nil {
		t.Fatal(err)
	}
	attached := func() []string {
		a, err := art.GetArtifact(ctx, logo.ID)
		if err != nil {
			t.Fatal(err)
		}
		return a.AttachedTicketIDs
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan, Artifacts: art,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, Documents: plan, Artifacts: art,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the pages' event streams never end on their own
		srv.Close()
	})

	// Two people on the same project, each with their own browser: one on the
	// library, one on a task's design assets.
	curator, worker := newPerson(t), newPerson(t)
	library := &tab{t: t, p: curator, ctx: curator.ctx}
	task := &tab{t: t, p: worker, ctx: worker.ctx}

	const lib = `document.querySelector('main[x-data="sessionsDesignLibrary"]')`
	const page = `document.querySelector('main[x-data="sessionsTaskDesign"]')`
	attachedCards := page + `.querySelectorAll('[data-group="attached"] .artifact-card').length`

	// The task's page first: nothing made, nothing attached yet.
	task.run("open the task's design assets",
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/"+p.ID+"/tasks/"+checkout.ID+"/design"),
		waitFor(page+` && document.body.textContent.includes('Nothing designed or attached yet')`),
	)

	// The library, by keyboard: open the picker on the logo, filter, Enter.
	library.run("open the library",
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/"+p.ID+"/design"),
		waitFor(lib+`.querySelectorAll('.artifact-card').length === 1`),
		waitFor(`document.querySelector('[data-state="live"]') !== null`),
		both("design-library"),
	)
	library.run("attach by keyboard",
		chromedp.Evaluate(`document.querySelector('button[aria-label="Attach Logo to a task"]').focus()`, nil),
		chromedp.KeyEvent("\r"),
		waitFor(`document.querySelector('dialog.palette')?.open === true && document.activeElement?.getAttribute('role') === 'combobox'`),
	)
	library.run("screenshot the picker", both("design-library-picker"))
	library.run("filter the picker",
		chromedp.KeyEvent("check"),
		waitFor(`[...document.querySelectorAll('dialog.palette [role=option]')].map(o => o.textContent.trim()).join() === 'Checkout'`),
	)
	library.run("pick with Enter",
		pressEnter(),
		waitFor(`document.querySelector('dialog.palette')?.open === false`),
		waitFor(`document.activeElement?.getAttribute('aria-label') === 'Attach Logo to a task'`),
		waitFor(lib+`.querySelector('[data-attached-task] a')?.textContent === 'Checkout'`),
	)
	if got := attached(); !slices.Equal(got, []string{checkout.ID}) {
		t.Fatalf("after the attach the server holds %v", got)
	}

	// The task's page saw it over the feed, in its own group, naming where it came from.
	task.run("the attach shows on the task's page",
		waitFor(attachedCards+` === 1`),
		waitFor(page+`.querySelector('[data-group="attached"] [data-task]').textContent.includes('from Brand refresh')`),
	)
	task.run("screenshot the attached asset", both("task-design-attached"))

	// Detach from the task's page: it leaves here, and the library's row goes too.
	task.run("detach from the task's page",
		chromedp.Evaluate(`document.querySelector('main[x-data="sessionsTaskDesign"] [data-group="attached"] .artifact-card').click()`, nil),
		clickText(`main[x-data="sessionsTaskDesign"] .preview`, "Detach from this task"),
		waitFor(attachedCards+` === 0`),
	)
	library.run("the library sees the detach", waitFor(lib+`.querySelector('[data-attached-task]') === null`))
	if got := attached(); len(got) != 0 {
		t.Fatalf("after the detach the server holds %v", got)
	}

	// Attach it again from the task's page, through its picker of the project's assets.
	task.run("attach from the task's page",
		clickText(`main[x-data="sessionsTaskDesign"] header`, "Attach a project asset…"),
		waitFor(`[...document.querySelectorAll('dialog.palette [role=option]')].map(o => [...o.querySelectorAll('span')].map(x => x.textContent).join(' | ')).join() === 'Logo | page · from Brand refresh'`),
		pressEnter(),
		waitFor(attachedCards+` === 1`),
	)

	// Moving it back to its task asks first, naming the task it leaves; the task's page drops it live.
	var question string
	library.run("move back, after the warning",
		waitFor(lib+`.querySelector('[data-attached-task] a')?.textContent === 'Checkout'`),
		clickText(`main[x-data="sessionsDesignLibrary"] .preview`, "Move back to task"),
		waitFor(`!!document.querySelector('[role=alertdialog] strong')`),
		chromedp.Evaluate(`document.querySelector('[role=alertdialog] strong').textContent`, &question),
		clickText(`[role=alertdialog]`, "Move back"),
		waitFor(`!!document.querySelector('[data-moved-back] a')`),
	)
	if question != "Move Logo back to Brand refresh? It will be detached from 1 task: Checkout." {
		t.Errorf("move-back question = %q", question)
	}
	task.run("the move back shows on the task's page",
		waitFor(attachedCards+` === 0`),
		waitFor(`document.querySelector('[role=status][aria-live=polite]').textContent === 'Moved back to its task: Logo'`),
	)

	// The task page's toolbar counts its assets; the producer's counts its own logo.
	var count string
	task.run("the toolbar counts the task's assets",
		chromedp.Navigate(srv.URL+"/projects/"+p.ID+"/tasks/"+brand.ID),
		waitFor(`!!document.querySelector('[data-design-assets] .badge')`),
		chromedp.Evaluate(`document.querySelector('[data-design-assets] .badge').textContent`, &count),
	)
	if count != "1" {
		t.Errorf("Brand refresh's design assets count = %q, want its logo", count)
	}

	// The wire refuses to detach an asset from the task that made it.
	var code string
	task.run("detach from the producing task is refused",
		chromedp.Evaluate(`fetch('/api/detach_artifact_from_ticket', {method: 'POST', headers: {'Content-Type': 'application/json'},
			body: JSON.stringify({artifact_id: '`+logo.ID+`', ticket_id: '`+brand.ID+`'})}).then(r => r.json()).then(b => b.code)`, &code, awaitPromise),
	)
	if code != "ARTIFACT_PRODUCER_TASK" {
		t.Errorf("detach from the producer = %q", code)
	}

	// At 375px neither page scrolls sideways, and the picker fits. The logo
	// goes back to the project first (an agent's move), so both pages list it.
	if _, err := art.SetArtifactScope(ctx, logo.ID, domain.ArtifactScopeProject); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/projects/" + p.ID + "/design", "/projects/" + p.ID + "/tasks/" + brand.ID + "/design"} {
		var overflow, dialogWidth float64
		task.run("375px "+path,
			chromedp.EmulateViewport(375, 812),
			chromedp.Navigate(srv.URL+path),
			waitFor(`document.querySelectorAll('.artifact-card').length === 1`),
			chromedp.Evaluate(`document.documentElement.scrollWidth - document.documentElement.clientWidth`, &overflow),
		)
		if overflow > 0 {
			t.Errorf("at 375px %s scrolls sideways by %vpx", path, overflow)
		}
		if strings.HasSuffix(path, "/design") && strings.Contains(path, "/tasks/") {
			task.run("375px picker",
				clickText(`main header`, "Attach a project asset…"),
				waitFor(`document.querySelector('dialog.palette')?.open === true`),
				chromedp.Evaluate(`document.querySelector('dialog.palette').getBoundingClientRect().width`, &dialogWidth),
				shot("task-design-375-picker", "light"),
				chromedp.KeyEvent("\x1b"),
				waitFor(`document.querySelector('dialog.palette')?.open === false`),
			)
			if dialogWidth > 375-16 {
				t.Errorf("at 375px the picker is %vpx wide", dialogWidth)
			}
		}
	}

	// The theme follows the person's scheme.
	var light, dark string
	task.run("light and dark",
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "light"}}),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.body).backgroundColor`, &light),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "dark"}}),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.body).backgroundColor`, &dark),
	)
	if light == dark {
		t.Errorf("the page looks the same in light and dark: %s", light)
	}

	for _, p := range []*person{curator, worker} {
		p.mu.Lock()
		if len(p.errors) > 0 {
			t.Errorf("JS errors: %v", p.errors)
		}
		p.mu.Unlock()
	}
}

// tab drives a person's browser, one step at a time.
type tab struct {
	t   *testing.T
	p   *person
	ctx context.Context
}

// run runs one step, given 30s: chromedp's selector-based actions can wait
// forever once a modal <dialog> is open (it does not handle the top-layer
// event), so the steps use JS and key events, and a hang names its step.
func (b *tab) run(step string, actions ...chromedp.Action) {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, actions...); err != nil {
		var page string
		_ = chromedp.Run(b.ctx, chromedp.Evaluate(`document.querySelector('main')?.innerText.slice(0, 3000) ?? ''`, &page))
		b.p.mu.Lock()
		defer b.p.mu.Unlock()
		b.t.Fatalf("%s: %v\npage:\n%s\nJS errors: %v", step, err, page, b.p.errors)
	}
}

// clickText clicks the button inside scope (a JS expression or selector) whose text is text.
func clickText(scope, text string) chromedp.Action {
	root := scope
	if !strings.HasPrefix(scope, "document.") {
		root = `document.querySelector(` + jsString(scope) + `)`
	}
	js := `(() => { const b = [...(` + root + `?.querySelectorAll('button') ?? [])].find(b => b.textContent.trim() === ` + jsString(text) + `);
		if (!b) return false; b.click(); return true; })()`
	return waitFor(js)
}

// waitFor polls js every 100ms for up to 10s. Not on animation frames (the
// default): the person has two tabs, and a background tab gets none.
func waitFor(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingInterval(100*time.Millisecond), chromedp.WithPollingTimeout(10*time.Second))
}

func jsString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }

// pressEnter presses Enter as a browser delivers it to a keydown handler that
// prevents it: keydown and keyup, no keypress. chromedp's KeyEvent sends the
// keypress ("char") on its own, after the keydown handler has run, so when the
// picker closes on keydown and gives focus back to the button that opened it,
// that stray keypress would click the button and open the picker again.
func pressEnter() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		down := input.DispatchKeyEvent(input.KeyRawDown).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13)
		if err := down.Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13).Do(ctx)
	})
}
