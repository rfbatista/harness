//go:build !windows

package app

import (
	"context"
	"errors"
	"fmt"
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
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The architect channel in the web UI, end to end: the server as it runs (a
// shell standing in for claude), its HTTP API and project feed, and the web
// pages, on one test server; a headless Chrome is the person. Agents act
// through the real task tools. Screenshots land in ARCHITECT_SHOTS when it
// names a directory.

type archWeb struct {
	t       *testing.T
	ctx     context.Context
	orch    *orchestration.Service
	plan    *planning.Service
	channel *taskchannel.Service
	srv     *httptest.Server
	tools   map[string]domain.Tool
	project string
	repo    string
	ticket  string
}

func newArchWeb(t *testing.T) *archWeb {
	t.Helper()
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &archWeb{t: t, ctx: context.Background()}
	var (
		cat      catalog.Catalog
		sessions ports.SessionRepository
	)
	// Without ExecutionModule: it registers Genkit flows in a process-wide
	// registry, so it cannot boot twice in one test binary, and the
	// architect channel's own end-to-end test boots it already.
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&e.orch, &e.plan, &e.channel, &cat, &sessions),
		fx.NopLogger,
	)
	if err := app.Start(e.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := cat.Projects.CreateProject(e.ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(e.ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := e.plan.CreateTicket(e.ctx, p.ID, "Architect highlights", "Highlight the architect; let delegates talk to it; let it ask the person for reviews.", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	e.project, e.repo, e.ticket = p.ID, r.ID, tk.ID

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, Agents: cat.Agents, Sessions: e.orch, Planning: e.plan, TaskChannel: e.channel,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: e.plan, Sessions: e.orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, Documents: e.plan,
	}, assets, nil))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		e.srv.CloseClientConnections() // the pages' event streams never end on their own
		e.srv.Close()
	})

	e.tools = map[string]domain.Tool{}
	for _, tool := range tooling.SessionTaskTools(e.plan, sessions, cat.Agents, cat.Architecture,
		tooling.PeerStarter{Sessions: e.orch, Repositories: cat.Projects}, tooling.ArtifactTooling{}, e.channel) {
		e.tools[tool.Name] = tool
	}
	return e
}

// as calls a task tool as session id, the way the per-session MCP endpoint does.
func (e *archWeb) as(id, name string, args map[string]any) map[string]any {
	e.t.Helper()
	tool, ok := e.tools[name]
	if !ok {
		e.t.Fatalf("no tool %s", name)
	}
	res, err := tool.Handler(tooling.WithSessionID(e.ctx, id), args)
	if err != nil {
		e.t.Fatalf("%s as %s: %v", name, id, err)
	}
	out, _ := res.(map[string]any)
	if out == nil {
		b := fmt.Sprintf("%v", res)
		return map[string]any{"raw": b}
	}
	return out
}

func (e *archWeb) url(path string) string { return e.srv.URL + path }

// checkState asserts the task's one status-check loop, as the server holds it.
func (e *archWeb) checkState(state domain.StatusCheckState, every int) {
	e.t.Helper()
	checks, err := e.channel.ListStatusChecks(e.ctx, e.ticket)
	if err != nil {
		e.t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].State != state || checks[0].EveryMinutes != every {
		e.t.Errorf("the loop: %+v, want %s every %d", checks, state, every)
	}
}

func hasNote(list []*domain.ReviewRequest, note string) bool {
	for _, r := range list {
		if r.ResponseNote == note {
			return true
		}
	}
	return false
}

// person is a headless Chrome, recording uncaught errors.
type person struct {
	ctx    context.Context
	mu     sync.Mutex
	errors []string
}

func newPerson(t *testing.T) *person {
	t.Helper()
	ctx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-extensions", true))...)
	ctx, cancelBrowser := chromedp.NewContext(ctx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(func() { cancelTimeout(); cancelBrowser(); cancelAlloc() })
	p := &person{ctx: ctx}
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*runtime.EventExceptionThrown); ok {
			p.mu.Lock()
			p.errors = append(p.errors, e.ExceptionDetails.Error())
			p.mu.Unlock()
		}
	})
	if err := chromedp.Run(ctx); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) || strings.Contains(err.Error(), "executable file not found") {
			t.Skipf("browser test: Chrome not found (%v)", err)
		}
		t.Fatal(err)
	}
	return p
}

func (p *person) run(t *testing.T, step string, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(p.ctx, actions...); err != nil {
		var page string
		_ = chromedp.Run(p.ctx, chromedp.Evaluate(`document.querySelector('main')?.innerText.slice(0, 3000) ?? ''`, &page))
		t.Fatalf("%s: %v\npage:\n%s\nJS errors: %v", step, err, page, p.errors)
	}
}

func (p *person) text(t *testing.T, js string) string {
	t.Helper()
	var s string
	p.run(t, "read "+js, chromedp.Evaluate(js, &s))
	return s
}

func poll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(10*time.Second))
}

// shot saves the page as name.png in ARCHITECT_SHOTS, in the given color
// scheme; without ARCHITECT_SHOTS it does nothing.
func shot(name, scheme string) chromedp.Action {
	dir := os.Getenv("ARCHITECT_SHOTS")
	if dir == "" {
		return chromedp.ActionFunc(func(context.Context) error { return nil })
	}
	var buf []byte
	return chromedp.Tasks{
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}}),
		chromedp.Sleep(150 * time.Millisecond), // the theme's transition
		chromedp.FullScreenshot(&buf, 90),
		chromedp.ActionFunc(func(context.Context) error { return os.WriteFile(filepath.Join(dir, name+".png"), buf, 0o644) }),
	}
}

// both saves name-light and name-dark.
func both(name string) chromedp.Action {
	return chromedp.Tasks{shot(name+"-light", "light"), shot(name+"-dark", "dark")}
}

func TestArchitectChannelInTheWebUI_EndToEnd(t *testing.T) {
	e := newArchWeb(t)
	p := newPerson(t)

	// The person starts an architect; it delegates the server side with a
	// status-check loop, and the web UI side without one.
	arch, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.ticket, Mode: "architect", Prompt: "Shape the architect highlights."})
	if err != nil {
		t.Fatal(err)
	}
	server := e.as(arch.ID, "start_task_session", map[string]any{"prompt": "Server: the architect channel.", "status_check_minutes": float64(10)})["session_id"].(string)
	webUI := e.as(arch.ID, "start_task_session", map[string]any{"prompt": "Web UI: the review inbox.", "status_check_minutes": float64(0)})["session_id"].(string)

	task := e.url("/projects/" + e.project + "/tasks/" + e.ticket)
	rows := `[...document.querySelectorAll('[role=listbox] .row')]`
	row := func(id string) string {
		return fmt.Sprintf(`document.querySelector('[role=listbox] .row[data-session-id=%q]')`, id)
	}
	p.run(t, "open the task",
		chromedp.EmulateViewport(1280, 860),
		chromedp.Navigate(task),
		poll(rows+`.length === 3 && !!document.querySelector('[role=listbox] .row[data-role=architect]')`),
	)
	order := p.text(t, rows+`.map(r => r.dataset.sessionId + ':' + r.dataset.role + ':' + (r.dataset.depth ?? '0')).join(' ')`)
	if want1, want2 := arch.ID+":architect:0 "+webUI+":delegate:1 "+server+":delegate:1", arch.ID+":architect:0 "+server+":delegate:1 "+webUI+":delegate:1"; order != want1 && order != want2 {
		t.Errorf("rows: %s", order)
	}
	if badge := p.text(t, row(arch.ID)+`.querySelector('.title .badge').textContent`); badge != "architect" {
		t.Errorf("the architect's badge: %q", badge)
	}
	if meta := p.text(t, row(server)+`.querySelector('.meta').textContent`); !strings.Contains(meta, "check in 9m") && !strings.Contains(meta, "check in 10m") {
		t.Errorf("the server delegate's meta lacks its next check: %q", meta)
	}

	// The server delegate reports live; its row says so.
	e.as(server, "message_architect", map[string]any{"kind": "status_report", "status": "ready_for_review", "body": "The port and the scheduler are done.\nTests are green."})
	p.run(t, "a report arrives", poll(row(server)+`.querySelector('.meta').textContent.includes('ready for review')`))

	// The architect's Conversation shows it; the architect replies, live.
	p.run(t, "open the architect's conversation",
		chromedp.Evaluate(row(arch.ID)+`.click()`, nil),
		poll(`[...document.querySelectorAll('[role=tab]')].some(b => b.textContent.trim().startsWith('Conversation'))`),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=tab]')].find(b => b.textContent.trim().startsWith('Conversation')).click()`, nil),
		poll(`document.querySelectorAll('[aria-label=Conversation] .message').length === 1`),
	)
	e.as(arch.ID, "reply_to_session", map[string]any{"session_id": server, "body": "Good. Rename SetStatusCheck's argument, then report done."})
	p.run(t, "the reply arrives",
		poll(`document.querySelectorAll('[aria-label=Conversation] .message').length === 2`),
		both("1-conversation"),
	)
	conv := p.text(t, `document.querySelector('[aria-label=Conversation]').innerText`)
	for _, want := range []string{"→ the architect", "status report", "ready for review", "the architect →", "reply", "Good. Rename"} {
		if !strings.Contains(conv, want) {
			t.Errorf("the conversation lacks %q:\n%s", want, conv)
		}
	}

	// The person pauses the server delegate's checks.
	p.run(t, "pause the delegate's checks",
		chromedp.Evaluate(row(server)+`.click()`, nil),
		poll(`!!document.querySelector('[aria-label="Status checks"] button')`),
		both("2-status-check"),
		chromedp.Click(`[aria-label="Status checks"] button`, chromedp.ByQuery),
		poll(`document.querySelector('[aria-label="Status checks"]').textContent.includes('checks paused')`),
		poll(row(server)+`.querySelector('.meta').textContent.includes('checks paused')`),
	)
	e.checkState(domain.StatusCheckPaused, 0)
	p.run(t, "resume them",
		chromedp.Click(`[aria-label="Status checks"] button`, chromedp.ByQuery),
		poll(`document.querySelector('[aria-label="Status checks"]').textContent.includes('checks every 10m')`),
	)
	e.checkState(domain.StatusCheckActive, 10)

	// The architect asks the person for a review: the band shows it live.
	e.as(arch.ID, "request_user_review", map[string]any{"subject": "Server plan ready for sign-off", "body": "Please check the status authority rule\nand the status-check defaults.", "about_session_id": server})
	band := `document.querySelector('[aria-label="Review requests"]')`
	p.run(t, "a review request arrives",
		poll(`!!`+band+` && !`+band+`.hidden && `+band+`.querySelectorAll('.review').length === 1`),
		both("3-review-band"),
	)
	if got := p.text(t, band+`.innerText`); !strings.Contains(got, "1 review waits on you") || !strings.Contains(got, "about plain claude · Architect highlights") {
		t.Errorf("the band reads:\n%s", got)
	}

	// Changes without a note are stopped at the field; with one, the
	// architect gets them.
	p.run(t, "request changes without a note",
		chromedp.Evaluate(band+`.querySelectorAll('.review button')[1].click()`, nil),
		poll(`!!`+band+`.querySelector('textarea[aria-invalid=true]')`),
	)
	if got := p.text(t, band+`.querySelector('.field .error').textContent`); got != "Say what should change: the architect passes it on." {
		t.Errorf("the note's error: %q", got)
	}
	if reviews, _ := e.channel.ListReviewRequests(e.ctx, e.ticket, domain.ReviewPending); len(reviews) != 1 {
		t.Errorf("nothing should have been sent: %+v", reviews)
	}
	p.run(t, "request changes",
		chromedp.SendKeys(`[aria-label="Review requests"] textarea`, "Split step 6 in two.", chromedp.ByQuery),
		chromedp.Evaluate(band+`.querySelectorAll('.review button')[1].click()`, nil),
		poll(band+`.querySelector('details summary')?.textContent === 'Earlier reviews (1)'`),
		chromedp.Evaluate(band+`.querySelector('details summary').click()`, nil),
		both("4-review-answered"),
	)
	reviews, err := e.channel.ListReviewRequests(e.ctx, e.ticket, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].State != domain.ReviewChangesRequested || reviews[0].ResponseNote != "Split step 6 in two." {
		t.Errorf("the request after the person's answer: %+v", reviews)
	}

	// The architect moves the task with a reason: the page says so and the
	// picker follows.
	e.as(arch.ID, "update_task_status", map[string]any{"status": "review", "reason": "Server and tools merged; the web UI is in review."})
	p.run(t, "the architect moves the task",
		poll(`!!document.querySelector('.status-change') && document.querySelector('.toolbar .status-picker').value === 'review'`),
	)
	if got := p.text(t, `document.querySelector('.status-change').textContent.replace(/\s+/g, ' ').trim()`); got != "Moved to review by the architect · now — “Server and tools merged; the web UI is in review.”" {
		t.Errorf("status line: %q", got)
	}

	// The task page on a phone.
	p.run(t, "a phone",
		chromedp.EmulateViewport(375, 812),
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		both("5-task-phone"),
	)
	if overflow := p.text(t, `String(document.documentElement.scrollWidth - document.documentElement.clientWidth)`); overflow != "0" {
		t.Errorf("at 375px the page scrolls sideways by %spx", overflow)
	}
	p.run(t, "back to the desktop", chromedp.EmulateViewport(1280, 860))

	// Two more requests: the board says so, and its badge goes down live when
	// one is answered elsewhere (another tab, over the API).
	e.as(arch.ID, "request_user_review", map[string]any{"subject": "Review inbox wording", "body": "Does the band read well?", "about_session_id": webUI})
	e.as(arch.ID, "request_user_review", map[string]any{"subject": "Board badge wording", "body": "One review or one request?"})
	badge := `(document.querySelector('.board .card .badge[data-tone=attention]')?.textContent ?? '')`
	p.run(t, "the board",
		chromedp.Navigate(e.url("/projects/"+e.project)),
		poll(`(`+badge+`) === '2 reviews'`),
		both("6-board"),
	)
	pending, err := e.channel.ListReviewRequests(e.ctx, e.ticket, domain.ReviewPending)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending: %v %+v", err, pending)
	}
	var wording string
	for _, r := range pending {
		if r.Subject == "Board badge wording" {
			wording = r.ID
		}
	}
	res, err := http.Post(e.url("/api/respond_review_request"), "application/json", strings.NewReader(`{"review_id":"`+wording+`","decision":"approved","note":""}`))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("approve elsewhere: %v %v", err, res)
	}
	res.Body.Close()
	p.run(t, "the badge goes down", poll(`(`+badge+`) === '1 review'`), poll(`document.querySelector('nav [x-text=reviewsCount]').textContent === '1'`))

	// The inbox, keyboard only: from the note, Tab reaches Approve, Enter answers.
	p.run(t, "the inbox",
		chromedp.Navigate(e.url("/projects/"+e.project+"/reviews")),
		poll(`document.querySelectorAll('main .review').length === 1`),
		both("7-inbox"),
		chromedp.Focus(`main .review textarea`, chromedp.ByQuery),
		chromedp.SendKeys(`main .review textarea`, "Reads well.", chromedp.ByQuery),
		chromedp.KeyEvent("\t"),
	)
	if focused := p.text(t, `document.activeElement.textContent.trim()`); focused != "Approve" {
		t.Fatalf("Tab from the note lands on %q, want Approve", focused)
	}
	p.run(t, "approve with Enter",
		chromedp.KeyEvent("\r"),
		poll(`!!document.querySelector('main .empty')`),
		both("8-inbox-empty"),
	)
	if got := p.text(t, `document.querySelector('main .toolbar').innerText`); !strings.Contains(got, "0 reviews wait on you") {
		t.Errorf("the inbox after approving: %q", got)
	}
	if reviews, _ := e.channel.ListReviewRequests(e.ctx, e.ticket, domain.ReviewApproved); len(reviews) != 2 || !hasNote(reviews, "Reads well.") {
		t.Errorf("approved: %+v", reviews)
	}
	p.run(t, "the board, nothing waiting",
		chromedp.Navigate(e.url("/projects/"+e.project)),
		poll(`!!document.querySelector('.board .card')`),
	)
	if got := p.text(t, badge); got != "" {
		t.Errorf("the board still shows %q", got)
	}

	// Keyboard only on the task page: J and K walk the architect's group.
	p.run(t, "J and K",
		chromedp.Navigate(task),
		// Alpine has taken over the list: the server's copy is gone.
		poll(`!!document.querySelector('[role=listbox] .row[data-role=architect][aria-selected=true]') && !document.querySelector('[role=listbox] [data-ssr]')`),
		chromedp.Focus(`[role=listbox]`, chromedp.ByQuery),
		chromedp.KeyEvent("j"),
		poll(`document.querySelector('[role=listbox] .row[aria-selected=true]')?.dataset.role === 'delegate'`),
		// A delegate runs in a terminal on the server, and the terminal takes
		// the focus when its first screen arrives (sessionsTerminal). The person
		// comes back to the list before walking on.
		poll(`!!document.activeElement?.closest('.terminal')`),
		chromedp.Focus(`[role=listbox]`, chromedp.ByQuery),
		chromedp.KeyEvent("k"),
		poll(`document.querySelector('[role=listbox] .row[aria-selected=true]')?.dataset.role === 'architect'`),
	)

	if banners := p.text(t, `[...document.querySelectorAll('.banner[role=alert]')].map(b => b.innerText).join(' | ')`); banners != "" {
		t.Errorf("an error banner shows: %s", banners)
	}
	if len(p.errors) > 0 {
		t.Errorf("JS errors: %v", p.errors)
	}
}
