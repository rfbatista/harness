//go:build !windows

package web_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
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

// A task's resources and comments in the web UI, end to end: the server as
// it runs (planning with its resources, comments and blob store, the project
// feed, the HTTP API and the pages) and a person in headless Chrome. They
// upload a file through the picker and add a link, rename and delete in the
// row, comment, and look at an image in the lightbox; what another client
// adds arrives live, and nothing shows twice. The task page's counts follow.
// Screenshots land in PROJECT_SHOTS when it names a directory.
func TestTaskResourcesAndCommentsInTheWebUI_EndToEnd(t *testing.T) {
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
		cat  catalog.Catalog
		orch *orchestration.Service
		plan *planning.Service
	)
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "data.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&cat, &orch, &plan),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })

	project, err := cat.Projects.CreateProject(ctx, "harness", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := plan.CreateTicket(ctx, project.ID, "Add assets and comments", "", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	image, err := plan.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: task.ID, FileName: "wireframe.png", Mime: "image/png", Content: bytes.NewReader(tinyPNG())})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.AddTicketComment(ctx, task.ID, "The order:\n1. the spec\n2. the wireframe"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Sessions: orch, Planning: plan,
		TaskResources: plan, TaskComments: plan,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects,
		Resources: plan, Comments: plan,
	}, assets, nil))
	// HTTP/2: the pages hold event streams open, and HTTP/1.1 allows Chrome six connections per host.
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	taskURL := srv.URL + "/projects/" + project.ID + "/tasks/" + task.ID
	pageURL := taskURL + "/resources"

	const main = `document.querySelector('main[x-data="tasksResources"]')`
	titles := `[...document.querySelectorAll('main .resources-list .resource:not([data-upload]) .title')].map(e => (e.firstElementChild ?? e).textContent.trim()).join('|')`
	bodies := `[...document.querySelectorAll('main .thread .comment .body')].map(e => e.textContent).join('|')`
	toasts := `[...document.querySelectorAll('.toast')].map(e => e.textContent).join('|')`
	rowOf := func(title string) string {
		return `[...document.querySelectorAll('main .resource')].find(li => li.querySelector('.title') && (li.querySelector('.title').firstElementChild ?? li.querySelector('.title')).textContent.trim() === (` + quote(title) + `))`
	}
	menuItem := func(title, label string) chromedp.Action {
		return waitUntil(`(() => { const row = ` + rowOf(title) + `; if (!row) return false;
			row.querySelector('.menu > button').click();
			const item = [...row.querySelectorAll('[role=menuitem]')].find(i => i.textContent.trim() === ` + quote(label) + `);
			if (!item) return false; item.click(); return true; })()`)
	}

	p := newTLSBrowser(t)
	p.run("the task page links its resources and comments with their counts",
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(taskURL),
		waitUntil(`document.querySelector('[data-task-resources] .badge')?.textContent === '1'`),
		waitUntil(`document.querySelector('[data-task-comments] .badge')?.textContent === '1'`),
	)
	if _, err := plan.AddTicketLink(ctx, task.ID, "https://example.com/spec", "Spec"); err != nil {
		t.Fatal(err)
	}
	p.run("a resource added elsewhere counts on the task page live",
		waitUntil(`document.querySelector('[data-task-resources] .badge')?.textContent === '2'`),
	)

	p.run("open the resources page from the toolbar",
		chromedp.Evaluate(`document.querySelector('[data-task-resources]').click()`, nil),
		atPath(pageURL),
		waitUntil(main+` !== null && (`+titles+`) === 'wireframe.png|Spec'`),
		waitUntil(`(`+bodies+`) === 'The order:\n1. the spec\n2. the wireframe'`),
		waitUntil(`document.querySelector('main [data-summary]').textContent === '2 resources · 1 comment'`),
		waitUntil(`document.querySelector('.stream-bar [data-state="live"]') !== null`),
		bothSchemes("resources-page"),
	)

	upload := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(upload, []byte("what the agents should read first"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.run("a file chosen in the picker uploads and shows once, with the told-about-it toast",
		chromedp.SetUploadFiles(`main [data-file-input]`, []string{upload}, chromedp.ByQuery),
		waitUntil(`(`+titles+`) === 'wireframe.png|Spec|notes.txt'`),
		waitUntil(`document.querySelectorAll('main [data-upload]').length === 0`),
		waitUntil(`(`+toasts+`).includes('Added “notes.txt”. Sessions running on this task are told about it.')`),
		chromedp.Sleep(300*time.Millisecond),
		waitUntil(`(`+titles+`) === 'wireframe.png|Spec|notes.txt'`), // the feed echo did not add a second row
	)
	if list, _ := plan.ListTicketResources(ctx, task.ID); len(list) != 3 || list[2].Title != "notes.txt" || list[2].Kind != "file" {
		t.Fatalf("the server has %v after the upload", list)
	}

	big := filepath.Join(t.TempDir(), "take.mov")
	if err := os.WriteFile(big, make([]byte, domain.MaxResourceBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	p.run("a file over 25 MiB fails in its row before a byte is sent",
		chromedp.SetUploadFiles(`main [data-file-input]`, []string{big}, chromedp.ByQuery),
		waitUntil(`document.querySelector('main [data-upload][data-state="failed"] [role=alert]')?.textContent.includes('RESOURCE_TOO_LARGE · compress it or add a link')`),
		chromedp.Evaluate(`[...document.querySelectorAll('main [data-upload] button')].find(b => b.textContent.trim() === 'Dismiss').click()`, nil),
		waitUntil(`document.querySelectorAll('main [data-upload]').length === 0`),
	)
	if list, _ := plan.ListTicketResources(ctx, task.ID); len(list) != 3 {
		t.Fatalf("the refused file reached the server: %d resources", len(list))
	}

	p.run("a link that is not http(s) is refused at the field; a good one is added",
		chromedp.Evaluate(`document.querySelector('main [data-add-link]').click()`, nil),
		waitUntil(`document.activeElement === document.querySelector('main [data-link-url]')`),
		setField("main [data-link-url]", "figma.com/file/abc"),
		chromedp.Evaluate(`document.querySelector('#link-form').requestSubmit()`, nil),
		waitUntil(`document.querySelector('main [data-link-url]').getAttribute('aria-invalid') === 'true'`),
		waitUntil(`document.querySelector('#link-url-msg').textContent === 'Use an address that starts with http:// or https://.'`),
		setField("main [data-link-url]", "https://figma.com/file/abc"),
		setField("main [data-link-title]", "Figma file"),
		chromedp.Evaluate(`document.querySelector('#link-form').requestSubmit()`, nil),
		waitUntil(`(`+titles+`) === 'wireframe.png|Spec|notes.txt|Figma file'`),
		waitUntil(`document.querySelector('#link-form') === null && document.activeElement === document.querySelector('main [data-add-link]')`),
	)

	p.run("rename in place from the row menu: Enter saves",
		menuItem("Spec", "Rename"),
		waitUntil(`document.activeElement === document.querySelector('main [data-rename]')`),
		setField("main [data-rename]", "Spec v2"),
		pressKey("Enter", 13),
		waitUntil(`(`+titles+`) === 'wireframe.png|Spec v2|notes.txt|Figma file'`),
		waitUntil(`document.querySelector('main [data-rename]') === null`),
	)

	p.run("delete asks in the row; the person's own delete is not reported as elsewhere",
		menuItem("notes.txt", "Delete…"),
		waitUntil(`document.activeElement === document.querySelector('main [data-keep]')`),
		chromedp.Evaluate(`[...document.querySelectorAll('main .resource[data-confirming] button')].find(b => b.textContent.trim() === 'Delete').click()`, nil),
		waitUntil(`(`+titles+`) === 'wireframe.png|Spec v2|Figma file'`),
		waitUntil(`(`+toasts+`).includes('Deleted “notes.txt”.')`),
		waitUntil(`!(`+toasts+`).includes('elsewhere')`),
	)

	p.run("a comment sent with Ctrl Enter shows once and the composer clears",
		setField("main [data-composer]", "  Keep the 25 MiB limit visible.  "),
		chromedp.Evaluate(`document.querySelector('main [data-composer]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))`, nil),
		waitUntil(`(`+bodies+`) === 'The order:\n1. the spec\n2. the wireframe|Keep the 25 MiB limit visible.'`),
		waitUntil(`document.querySelector('main [data-composer]').value === ''`),
		chromedp.Sleep(300*time.Millisecond),
		waitUntil(`document.querySelectorAll('main .thread .comment').length === 2`),
	)

	if _, err := plan.AddTicketComment(ctx, task.ID, "From the other tab."); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.AddTicketLink(ctx, task.ID, "https://linear.app/x", "Linear"); err != nil {
		t.Fatal(err)
	}
	p.run("what another client adds arrives live, washed once",
		waitUntil(`document.querySelectorAll('main .thread .comment').length === 3`),
		waitUntil(`document.querySelector('main .thread .comment:last-child').hasAttribute('data-fresh')`),
		waitUntil(`(`+titles+`) === 'wireframe.png|Spec v2|Figma file|Linear'`),
		waitUntil(`document.querySelector('main [data-summary]').textContent === '4 resources · 3 comments'`),
	)

	p.run("an image opens in the lightbox with focus on Close; Esc closes it and focus goes back",
		chromedp.Evaluate(`document.querySelector('main .resource button.tile').click()`, nil),
		waitUntil(`document.querySelector('dialog.lightbox').open`),
		waitUntil(`document.querySelector('dialog.lightbox img').getAttribute('src') === '/api/ticket_resources/`+image.ID+`/content'`),
		waitUntil(`document.activeElement === document.querySelector('[data-lightbox-close]')`),
		waitUntil(`document.querySelector('#lightbox-name').textContent === 'wireframe.png'`),
		chromedp.KeyEvent(kb.Escape),
		waitUntil(`!document.querySelector('dialog.lightbox').open`),
		waitUntil(`document.activeElement === document.querySelector('main .resource button.tile')`),
	)

	p.run("at 375px the page is one column with no side scroll",
		chromedp.EmulateViewport(375, 812),
		chromedp.Navigate(pageURL),
		waitUntil(`(`+titles+`) === 'wireframe.png|Spec v2|Figma file|Linear'`),
		waitUntil(`document.documentElement.scrollWidth <= window.innerWidth`),
		waitUntil(`(() => { const bar = document.querySelector('main .toolbar').getBoundingClientRect();
			return bar.bottom <= document.querySelector('main .resources-col').getBoundingClientRect().top + 1; })()`),
		bothSchemes("resources-page-375"),
	)

	if errs := p.jsErrors(); len(errs) > 0 {
		t.Errorf("JS errors: %v", errs)
	}
}

// newTLSBrowser is a person's headless Chrome that accepts the test server's certificate.
func newTLSBrowser(t *testing.T) *browser {
	t.Helper()
	ctx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-extensions", true), chromedp.Flag("ignore-certificate-errors", true))...)
	ctx, cancelBrowser := chromedp.NewContext(ctx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 180*time.Second)
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

// pressKey sends a raw key down and up: a keydown handler sees it once,
// without the separate keypress chromedp.KeyEvent adds.
func pressKey(key string, code int64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchKeyEvent(input.KeyRawDown).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code).Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithKey(key).WithCode(key).WithWindowsVirtualKeyCode(code).Do(ctx)
	})
}

// tinyPNG is a 4×4 PNG.
func tinyPNG() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	return buf.Bytes()
}
