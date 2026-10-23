//go:build !windows

package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

// Comments written by sessions in the web UI, end to end: the server as it
// runs and a person in headless Chrome on the task's resources page. A
// session's comment paints from the seed and arrives live, names its agent
// ("Agent session" when it has none) with the Agent mark, links to its
// session while that is listed on the task, and offers Delete but no Edit;
// the server refuses an edit with COMMENT_NOT_YOURS. Screenshots land in
// PROJECT_SHOTS when it names a directory.
func TestAgentCommentsInTheWebUI_EndToEnd(t *testing.T) {
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
		sessions ports.SessionRepository
	)
	fxApp := fx.New(
		fx.Supply(app.Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "data.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		app.PersistenceModule, app.CatalogModule, app.PlanningModule, app.WorkspacesModule,
		app.AgentRuntimeModule, app.TaskChannelModule, app.TextProcessingModule, app.ToolingModule,
		fx.Populate(&cat, &orch, &plan, &sessions),
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
	task, err := plan.CreateTicket(ctx, project.ID, "Agent comments on tasks", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	// Two sessions listed on the task. s-other waits on the person, so the
	// task page would open it first; the comment's link must open s-rev.
	for _, s := range []*domain.Session{
		{ID: "s-rev", Status: domain.SessionDone, UpdatedAt: time.Now().Add(-time.Hour)},
		{ID: "s-other", Status: domain.SessionIdle, UpdatedAt: time.Now()},
	} {
		s.ProjectID, s.TicketID, s.Task = project.ID, task.ID, "work"
		if _, err := sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := plan.AddTicketComment(ctx, task.ID, "Use the v2 contract."); err != nil {
		t.Fatal(err)
	}
	seeded, err := plan.AddSessionComment(ctx, task.ID, domain.CommentWriter{SessionID: "s-rev", AgentName: "go-developer"}, "The server branch is ready.")
	if err != nil {
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
	taskPath := "/projects/" + project.ID + "/tasks/" + task.ID
	pageURL := srv.URL + taskPath + "/resources"

	authors := `[...document.querySelectorAll('main .thread .comment [data-author]')].map(e => e.textContent.trim()).join('|')`
	commentOf := func(id string) string {
		return `document.querySelector('main .thread [data-comment-id="` + id + `"]')`
	}
	buttonsOf := func(id string) string {
		return `[...` + commentOf(id) + `.querySelectorAll('header button')].map(b => b.textContent.trim()).join('|')`
	}

	p := newTLSBrowser(t)
	p.run("the seed paints the session's comment with its agent, the Agent mark and its session's link; no Edit",
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(pageURL),
		waitUntil(`(`+authors+`) === 'You|go-developer'`),
		waitUntil(commentOf(seeded.ID)+`.querySelector('[data-agent-mark]')?.textContent === 'Agent'`),
		waitUntil(commentOf(seeded.ID)+`.querySelector('[data-author] a')?.getAttribute('href') === '`+taskPath+`?session=s-rev'`),
		waitUntil(`(`+buttonsOf(seeded.ID)+`) === 'Delete'`),
		waitUntil(`document.querySelector('main .thread li.comment:first-of-type [data-agent-mark]') === null`),
		waitUntil(`[...document.querySelectorAll('main .thread li.comment:first-of-type header button')].map(b => b.textContent.trim()).join('|') === 'Edit|Delete'`),
		waitUntil(`document.querySelector('.stream-bar [data-state="live"]') !== null`),
		bothSchemes("agent-comments"),
	)

	live, err := plan.AddSessionComment(ctx, task.ID, domain.CommentWriter{SessionID: "s-gone"}, "A session with no agent, no longer listed.")
	if err != nil {
		t.Fatal(err)
	}
	p.run("a session's comment arrives live: Agent session, no link, announced",
		waitUntil(`(`+authors+`) === 'You|go-developer|Agent session'`),
		waitUntil(commentOf(live.ID)+`.hasAttribute('data-fresh')`),
		waitUntil(commentOf(live.ID)+`.querySelector('[data-author] a') === null`),
		waitUntil(`document.querySelector('main [role=status]').textContent.trim() === 'Agent session commented.'`),
		waitUntil(`(`+buttonsOf(live.ID)+`) === 'Delete'`),
	)

	var refused struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	p.run("the server refuses the person's edit of a session's comment",
		chromedp.Evaluate(`fetch('/api/update_ticket_comment', { method: 'POST', headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ comment_id: `+quote(live.ID)+`, body: 'mine now' }) })
			.then(async r => ({ status: r.status, code: (await r.json()).code }))`, &refused,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	)
	if refused.Status != http.StatusForbidden || refused.Code != "COMMENT_NOT_YOURS" {
		t.Fatalf("edit of a session's comment = %+v, want 403 COMMENT_NOT_YOURS", refused)
	}

	p.run("the person deletes a session's comment",
		chromedp.Evaluate(commentOf(live.ID)+`.querySelector('[data-delete]').click()`, nil),
		waitUntil(`document.activeElement === document.querySelector('main [data-keep]')`),
		clickButton(commentOf(live.ID), "Delete"),
		waitUntil(`(`+authors+`) === 'You|go-developer'`),
		waitUntil(`document.querySelector('main [data-summary]').textContent === '0 resources · 2 comments'`),
	)
	if list, _ := plan.ListTicketComments(ctx, task.ID, time.Time{}); len(list) != 2 {
		t.Fatalf("the server has %d comments after the delete, want 2", len(list))
	}

	p.run("the agent's name opens its session on the task page",
		chromedp.Evaluate(commentOf(seeded.ID)+`.querySelector('[data-author] a').click()`, nil),
		atPath(srv.URL+taskPath+"?session=s-rev"),
		waitUntil(`document.querySelector('.row[data-session-id="s-rev"][aria-selected="true"]') !== null`),
		waitUntil(`document.querySelector('.row[data-session-id="s-other"][aria-selected="true"]') === null`),
	)

	if errs := p.jsErrors(); len(errs) > 0 {
		t.Errorf("JS errors: %v", errs)
	}
}
