//go:build !windows

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/branchgate"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The branch request card in the web UI, end to end: the server as it runs, its
// API and project feed and the web pages on one test server, and a headless
// Chrome as the person. The agent's side is the asking hook, called as claude
// would call it (contract "Harness server ↔ Web UI — branch requests and the
// Branches list" §6).

type branchWeb struct {
	t       *testing.T
	ctx     context.Context
	orch    *orchestration.Service
	gate    *branchgate.Service
	notes   *notifications.Service
	srv     *httptest.Server
	project string
	ticket  string
	session *domain.Session
	dir     string
}

func newBranchWeb(t *testing.T) *branchWeb {
	t.Helper()
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
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := &branchWeb{t: t, ctx: context.Background()}
	var (
		cat  catalog.Catalog
		plan *planning.Service
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&e.orch, &plan, &cat, &e.gate, &e.notes),
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
	tk, err := plan.CreateTicket(e.ctx, p.ID, "Branch gate", "Gate the branches.", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	e.project, e.ticket = p.ID, tk.ID
	sess, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: p.ID, RepositoryID: r.ID, TicketID: tk.ID, Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	e.session, e.dir = sess, spec.Dir

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, Agents: cat.Agents, Sessions: e.orch, Planning: plan, BranchGate: e.gate,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: e.orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, Documents: plan, BranchRequests: e.gate, Notifications: e.notes,
	}, assets, nil))
	e.srv = httptest.NewUnstartedServer(mux)
	e.srv.EnableHTTP2 = true
	e.srv.StartTLS()
	t.Cleanup(func() {
		e.srv.CloseClientConnections()
		e.srv.Close()
	})
	return e
}

// ask runs the asking hook for command, as claude's Bash tool would, and gives
// the verdict once the person has answered.
func (e *branchWeb) ask(command, description string) <-chan ports.BranchHookVerdict {
	out := make(chan ports.BranchHookVerdict, 1)
	in := ports.BranchHookInput{ToolName: "Bash", Cwd: e.dir}
	in.ToolInput.Command, in.ToolInput.Description = command, description
	go func() { out <- e.gate.Hook(e.ctx, e.session.ID, in) }()
	return out
}

// pendingRequest waits for the n-th pending request, newest first.
func (e *branchWeb) pendingRequests(n int) []domain.BranchRequest {
	e.t.Helper()
	var got []domain.BranchRequest
	eventually(e.t, "pending branch requests", func() error {
		var err error
		got, err = e.gate.ListBranchRequests(e.ctx, domain.BranchRequestFilter{TaskID: e.ticket, State: domain.BranchRequestPending})
		if err != nil || len(got) != n {
			return http.ErrNotSupported
		}
		return nil
	})
	return got
}

func (e *branchWeb) verdict(ch <-chan ports.BranchHookVerdict) ports.BranchHookVerdict {
	e.t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		e.t.Fatal("the hook did not answer")
		return ports.BranchHookVerdict{}
	}
}

// clickInPopover clicks from the page: chromedp's own click scrolls the button
// into view first, which light-dismisses the popover before the click lands.
func clickInPopover(selector string) chromedp.Action {
	return chromedp.Evaluate(`document.querySelector(`+"`"+selector+"`"+`).click()`, nil)
}

const cardJS = `document.querySelector('#task-branch-requests article[data-branch-request]')`

func TestABranchRequestCardInTheBrowserAnswersTheAgent(t *testing.T) {
	e := newBranchWeb(t)
	p := newPerson(t)
	task := e.srv.URL + "/projects/" + e.project + "/tasks/" + e.ticket

	denied := e.ask("git checkout -b feature-x", "Try the new layout")
	e.pendingRequests(1)
	p.run(t, "open the task page", chromedp.EmulateViewport(1280, 860), chromedp.Navigate(task),
		poll(`getComputedStyle(document.querySelector('#task-branch-requests-button')).display !== 'none'`))
	if got := p.text(t, `document.querySelector('#task-branch-requests-button').getAttribute('aria-label')`); got != "Branch requests, 1 waiting" {
		t.Errorf("the button is named %q", got)
	}
	p.run(t, "open the popover", chromedp.Click(`#task-branch-requests-button`, chromedp.ByQuery),
		poll(`document.querySelector('#task-branch-requests').matches(':popover-open') && `+cardJS+` !== null`))
	card := p.text(t, cardJS+`.innerText`)
	for _, want := range []string{"Branch", "Create branch feature-x", "Starts from branch", "Try the new layout", "Repository", "session branch"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card lacks %q:\n%s", want, card)
		}
	}
	if tag := p.text(t, cardJS+`.querySelector('[data-branch-tag]').textContent`); tag != "Branch" {
		t.Errorf("tag = %q", tag)
	}
	p.run(t, "write the note", chromedp.SendKeys(`#task-branch-requests textarea`, "Stay on your branch", chromedp.ByQuery))
	p.run(t, "deny", clickInPopover(`#task-branch-requests button[data-action="deny"]`),
		poll(`!document.querySelector('#task-branch-requests').matches(':popover-open')`))
	v := e.verdict(denied)
	if v.Decision != "deny" || !strings.Contains(v.Reason, "Stay on your branch") {
		t.Errorf("the agent got %+v", v)
	}

	// A request that arrives while the page is open shows over the feed.
	approved := e.ask("git branch to-try", "")
	p.run(t, "the button comes back",
		poll(`getComputedStyle(document.querySelector('#task-branch-requests-button')).display !== 'none'`),
		chromedp.Click(`#task-branch-requests-button`, chromedp.ByQuery),
		poll(`document.querySelector('#task-branch-requests').matches(':popover-open') && `+cardJS+` !== null`),
		chromedp.SendKeys(`#task-branch-requests textarea`, "Fine", chromedp.ByQuery),
		clickInPopover(`#task-branch-requests button[data-action="approve"]`),
		poll(`!document.querySelector('#task-branch-requests').matches(':popover-open')`))
	if v := e.verdict(approved); v.Decision != "allow" || !strings.Contains(v.Context, "Fine") {
		t.Errorf("the agent got %+v", v)
	}

	// The inbox's link opens the card.
	inbox := e.ask("git branch -D old-branch", "")
	req := e.pendingRequests(1)[0]
	var link string
	eventually(t, "the inbox entry", func() error {
		page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{Limit: 50})
		if err != nil {
			return err
		}
		for _, n := range page.Notifications {
			if n.Kind == domain.NotificationBranchRequest && strings.HasSuffix(n.Link, "#branch-request-"+req.ID) {
				link = n.Link
				return nil
			}
		}
		return http.ErrNotSupported
	})
	p.run(t, "follow the inbox link", chromedp.Navigate(e.srv.URL+link),
		poll(`document.querySelector('#task-branch-requests').matches(':popover-open') && `+cardJS+` !== null`))
	if title := p.text(t, cardJS+`.querySelector('[data-branch-request-heading]').innerText`); !strings.Contains(title, "Delete branch old-branch") {
		t.Errorf("the card opened from the inbox is %q", title)
	}
	p.run(t, "approve", clickInPopover(`#task-branch-requests button[data-action="approve"]`))
	if v := e.verdict(inbox); v.Decision != "allow" {
		t.Errorf("the agent got %+v", v)
	}
	if len(p.errors) > 0 {
		t.Errorf("JS errors: %v", p.errors)
	}
}

func TestThePushAndRemoteCardsInTheBrowserShowWhereItGoesAndAnswerTheAgent(t *testing.T) {
	e := newBranchWeb(t)
	p := newPerson(t)
	task := e.srv.URL + "/projects/" + e.project + "/tasks/" + e.ticket

	bare := filepath.Join(t.TempDir(), "origin.git")
	for _, args := range [][]string{{"init", "-q", "--bare", bare}, {"-C", e.dir, "remote", "add", "origin", bare}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	session := e.session.Branch

	push := e.ask("git push origin "+session+":review/x", "Ready for review")
	e.pendingRequests(1)
	p.run(t, "open the task page", chromedp.EmulateViewport(1280, 860), chromedp.Navigate(task),
		poll(`getComputedStyle(document.querySelector('#task-branch-requests-button')).display !== 'none'`),
		chromedp.Click(`#task-branch-requests-button`, chromedp.ByQuery),
		poll(`document.querySelector('#task-branch-requests').matches(':popover-open') && `+cardJS+` !== null`))
	card := p.text(t, cardJS+`.innerText`)
	for _, want := range []string{"Branch", "Push branch " + session, "To origin (" + bare + ") as review/x", "commit", "against the base", "Ready for review"} {
		if !strings.Contains(card, want) {
			t.Errorf("the push card lacks %q:\n%s", want, card)
		}
	}
	p.run(t, "approve the push", clickInPopover(`#task-branch-requests button[data-action="approve"]`),
		poll(`!document.querySelector('#task-branch-requests').matches(':popover-open')`))
	if v := e.verdict(push); v.Decision != "allow" {
		t.Errorf("the agent got %+v", v)
	}

	// A remote the agent wants to add, with a credential in its URL: the card
	// shows it redacted and denying leaves the agent the note.
	add := e.ask("git remote add leak https://user:s3cret@example.com/x.git", "")
	e.pendingRequests(1)
	p.run(t, "the remote card arrives",
		poll(`getComputedStyle(document.querySelector('#task-branch-requests-button')).display !== 'none'`),
		chromedp.Click(`#task-branch-requests-button`, chromedp.ByQuery),
		poll(`document.querySelector('#task-branch-requests').matches(':popover-open') && `+cardJS+` !== null`))
	card = p.text(t, cardJS+`.innerText`)
	for _, want := range []string{"Add remote leak", "New URL https://example.com/x.git"} {
		if !strings.Contains(card, want) {
			t.Errorf("the remote card lacks %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "s3cret") {
		t.Errorf("the card shows the credential:\n%s", card)
	}
	p.run(t, "deny the remote", chromedp.SendKeys(`#task-branch-requests textarea`, "Use origin", chromedp.ByQuery),
		clickInPopover(`#task-branch-requests button[data-action="deny"]`),
		poll(`!document.querySelector('#task-branch-requests').matches(':popover-open')`))
	if v := e.verdict(add); v.Decision != "deny" || !strings.Contains(v.Reason, "Use origin") || strings.Contains(v.Reason, "s3cret") {
		t.Errorf("the agent got %+v", v)
	}
	if len(p.errors) > 0 {
		t.Errorf("JS errors: %v", p.errors)
	}
}
