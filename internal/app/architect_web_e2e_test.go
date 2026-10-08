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
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/artifacts"
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
		runner   *apps.Service
		art      *artifacts.Service
	)
	// Without ExecutionModule: it registers Genkit flows in a process-wide
	// registry, so it cannot boot twice in one test binary, and the
	// architect channel's own end-to-end test boots it already.
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&e.orch, &e.plan, &e.channel, &cat, &sessions, &runner, &art),
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
		RunCommands: cat.Projects, Apps: runner, Artifacts: art,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: e.plan, Sessions: e.orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, Documents: e.plan, Artifacts: art,
	}, assets, nil))
	// Over HTTP/2: the task page holds several event streams open, more than
	// Chrome's six HTTP/1.1 connections per host leave room for.
	e.srv = httptest.NewUnstartedServer(mux)
	e.srv.EnableHTTP2 = true
	e.srv.StartTLS()
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
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-extensions", true),
			chromedp.Flag("ignore-certificate-errors", true))...)
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
		poll(`!!document.querySelector('.status-change') && document.querySelector('.toolbar select[aria-label="Task status"]').value === 'review'`),
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
	res, err := e.srv.Client().Post(e.url("/api/respond_review_request"), "application/json", strings.NewReader(`{"review_id":"`+wording+`","decision":"approved","note":""}`))
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

	// Keyboard only on the task page: J and K walk the architect's group,
	// past the delegates' live terminals, at desktop and phone widths. A
	// terminal takes the focus only when the person asks for it.
	//
	// Each delegate's screen gets a few lines first, so the steps can tell
	// its snapshot has been drawn.
	for _, id := range []string{server, webUI} {
		term, err := e.orch.AttachTerminal(e.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := term.Paste("ready\r"); err != nil {
			t.Fatal(err)
		}
	}
	p.run(t, "open the task",
		chromedp.Navigate(task),
		// Alpine has taken over the list: the server's copy is gone.
		poll(`!!document.querySelector('[role=listbox] .row[data-role=architect][aria-selected=true]') && !document.querySelector('[role=listbox] [data-ssr]')`),
	)
	for _, size := range [][2]int64{{1280, 860}, {390, 844}} {
		walkWithKeys(t, p, server, webUI, size)
	}
	focusOnRequest(t, p, server)

	if banners := p.text(t, `[...document.querySelectorAll('.banner[role=alert]')].map(b => b.innerText).join(' | ')`); banners != "" {
		t.Errorf("an error banner shows: %s", banners)
	}
	if len(p.errors) > 0 {
		t.Errorf("JS errors: %v", p.errors)
	}
}

// Queries for the keyboard focus steps: the Agent tab's terminal, and the
// App tab's run terminal.
const (
	listFocused     = `document.activeElement === document.querySelector('[role=listbox]')`
	sessionTerminal = `document.querySelector('.terminal:not([aria-label=Application] .terminal)')`
	runTerminal     = `document.querySelector('[aria-label=Application] .terminal')`
	selectedRole    = `document.querySelector('[role=listbox] .row[aria-selected=true]')?.dataset.role`
	selectedID      = `document.querySelector('[role=listbox] .row[aria-selected=true]')?.dataset.sessionId`
)

// clickTab clicks the session detail's tab whose label starts with label.
func clickTab(label string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`[...document.querySelectorAll('[role=tab]')].find(b => b.textContent.trim().startsWith(%q)).click()`, label), nil)
}

// terminalDrawn waits for the selected session's terminal to be live and to
// have drawn its first snapshot.
func terminalDrawn(sessionID string) chromedp.Action {
	return chromedp.Tasks{
		poll(fmt.Sprintf(`%s === %q && %s?.querySelector('.status')?.dataset.state === 'running'`, selectedID, sessionID, sessionTerminal)),
		// A fresh screen has its cursor on the first row; the snapshot puts
		// it below the delegate's lines. xterm keeps its input under the
		// cursor, focused or not.
		poll(`parseFloat(` + sessionTerminal + `?.querySelector('.xterm-helper-textarea')?.style.top ?? '0') > 0`),
	}
}

// walkWithKeys walks the task's list with J and K, from the architect over
// both delegates and back, keeping the focus in the list while each
// delegate's terminal comes up live and draws (contract rules F1, F4).
func walkWithKeys(t *testing.T, p *person, first, second string, size [2]int64) {
	t.Helper()
	at := fmt.Sprintf(" at %dx%d", size[0], size[1])
	p.run(t, "focus the list"+at,
		chromedp.EmulateViewport(size[0], size[1]),
		poll(selectedRole+` === 'architect'`),
		chromedp.Focus(`[role=listbox]`, chromedp.ByQuery),
	)
	order := p.text(t, `[...document.querySelectorAll('[role=listbox] .row[data-role=delegate]')].map(r => r.dataset.sessionId).join(' ')`)
	if order != first+" "+second && order != second+" "+first {
		t.Fatalf("delegates%s: %q", at, order)
	}
	delegates := strings.Fields(order)
	for i, id := range delegates {
		p.run(t, fmt.Sprintf("J to delegate %d%s", i+1, at),
			chromedp.KeyEvent("j"),
			terminalDrawn(id),
		)
		if !p.truth(t, listFocused) {
			t.Fatalf("J to delegate %d%s: its terminal took the focus from the list (active: %s)", i+1, at, p.text(t, `document.activeElement?.className ?? ''`))
		}
	}
	p.run(t, "K back to delegate 1"+at,
		chromedp.KeyEvent("k"),
		terminalDrawn(delegates[0]),
	)
	if !p.truth(t, listFocused) {
		t.Fatalf("K back to delegate 1%s: its terminal took the focus from the list", at)
	}
	p.run(t, "K back to the architect"+at,
		chromedp.KeyEvent("k"),
		poll(selectedRole+` === 'architect'`),
	)
	if !p.truth(t, listFocused) {
		t.Fatalf("K back to the architect%s: the list lost the focus", at)
	}
}

// focusOnRequest checks that the terminal takes the focus when the person
// asks for it, and only then (contract rule F2): the App tab's run terminal
// draws without taking it and ignores the Agent tab's request; switching
// App → Agent, opening the Agent tab already in front, and clicking the
// screen each focus the session's terminal and nothing else.
func focusOnRequest(t *testing.T, p *person, delegate string) {
	t.Helper()
	// Every focus() on a terminal is recorded: which terminal took it.
	spy := `(() => {
		window.__terminalFocus = [];
		const focus = HTMLElement.prototype.focus;
		HTMLElement.prototype.focus = function (...args) {
			if (this.closest?.('.terminal')) window.__terminalFocus.push(this.closest('[aria-label=Application]') ? 'run' : 'session');
			return focus.apply(this, args);
		};
	})()`
	focused := func() string { return p.text(t, `window.__terminalFocus.join(' ')`) }
	inSession := `!!document.activeElement?.closest('.terminal') && !document.activeElement.closest('[aria-label=Application]')`

	p.run(t, "select the delegate, from the list",
		chromedp.EmulateViewport(1280, 860),
		chromedp.Evaluate(spy, nil),
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector('[role=listbox] .row[data-session-id=%q]').click()`, delegate), nil),
		terminalDrawn(delegate),
	)
	if got := focused(); got != "" {
		t.Fatalf("selecting a session focused a terminal: %q", got)
	}

	p.run(t, "run the app from the App tab",
		clickTab("App"),
		// Typed once the saved commands have loaded and the field is there;
		// Run is enabled once the command has reached the panel.
		poll(`(() => {
			const input = document.querySelector('[aria-label="Command to run"]');
			if (!input) return false;
			const command = "printf 'app is up\\n'; sleep 30";
			if (input.value !== command) {
				input.value = command;
				input.dispatchEvent(new Event('input', { bubbles: true }));
			}
			return ![...document.querySelectorAll('[aria-label=Application] button')].find(b => b.textContent.trim() === 'Run').disabled;
		})()`),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label=Application] button')].find(b => b.textContent.trim() === 'Run').click()`, nil),
		poll(runTerminal+`?.querySelector('.xterm-rows')?.textContent.includes('app is up')`),
		// The Agent tab's request, with no session terminal mounted: the run
		// terminal does not answer it.
		chromedp.Evaluate(`window.dispatchEvent(new CustomEvent('terminal-focus-requested'))`, nil),
	)
	if got := focused(); got != "" {
		t.Fatalf("the run terminal took the focus on its own: %q", got)
	}

	p.run(t, "App → Agent focuses the freshly mounted terminal",
		clickTab("Agent"),
		poll(inSession),
	)
	if got := focused(); got != "session" {
		t.Fatalf("App → Agent: focus() ran on %q, want the session terminal once", got)
	}

	p.run(t, "the Agent tab already in front focuses it again",
		chromedp.Focus(`[role=listbox]`, chromedp.ByQuery),
		poll(listFocused),
		clickTab("Agent"),
		poll(inSession),
	)
	p.run(t, "a click on the screen focuses it",
		chromedp.Focus(`[role=listbox]`, chromedp.ByQuery),
		poll(listFocused),
		chromedp.Click(`.terminal .screen`, chromedp.ByQuery),
		poll(inSession),
	)
	if got := focused(); strings.Contains(got, "run") {
		t.Fatalf("the run terminal took the focus: %q", got)
	}
}

func (p *person) truth(t *testing.T, js string) bool {
	t.Helper()
	var b bool
	p.run(t, "read "+js, chromedp.Evaluate(js, &b))
	return b
}
