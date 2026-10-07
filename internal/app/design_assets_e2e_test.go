package app

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A project asset attached over HTTP shows on the project feed with the task
// it is attached to; deleting that task detaches it, on the feed too.
func TestDesignAssetsAttachedToTasks_EndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		orch     *orchestration.Service
		plan     *planning.Service
		art      *artifacts.Service
		sessions ports.SessionRepository
	)
	// Without ExecutionModule: it registers Genkit flows in a process-wide
	// registry, so it cannot boot twice in one test binary.
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
	if orch.Artifacts == nil {
		t.Fatal("sessions are not briefed about their task's attached assets: the orchestration has no artifact reader")
	}

	p, err := cat.Projects.CreateProject(ctx, "harness", dir)
	if err != nil {
		t.Fatal(err)
	}
	producer, _ := plan.CreateTicket(ctx, p.ID, "Logo", "", domain.TicketStatusTodo)
	board, _ := plan.CreateTicket(ctx, p.ID, "Board", "", domain.TicketStatusTodo)
	worktree := t.TempDir()
	if _, err := sessions.Create(&domain.Session{ID: "s-design", ProjectID: p.ID, TicketID: producer.ID, Task: "logo", WorkingDir: worktree, Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "logo.html"), []byte("<p>logo</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	asset, err := art.Publish(ctx, ports.PublishArtifactRequest{SessionID: "s-design", Path: "logo.html", Title: "Logo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := art.SetArtifactScope(ctx, asset.ID, domain.ArtifactScopeProject); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Sessions: orch, Planning: plan, Artifacts: art})))
	t.Cleanup(srv.Close)
	e := &archE2E{t: t, ctx: ctx, srv: srv, project: p.ID}
	e.follow()

	status, out := e.http("POST", "/api/attach_artifact_to_ticket", `{"artifact_id":"`+asset.ID+`","ticket_id":"`+board.ID+`"}`)
	if status != 200 || out["artifact"] == nil {
		t.Fatalf("attach = %d %v", status, out)
	}
	e.sawOnFeed("artifact", `"id":"`+asset.ID+`"`, `"attached_ticket_ids":["`+board.ID+`"]`)

	if status, out := e.http("POST", "/api/delete_ticket", `{"ticket_id":"`+board.ID+`"}`); status >= 300 {
		t.Fatalf("delete ticket = %d %v", status, out)
	}
	e.sawOnFeed("artifact", `"id":"`+asset.ID+`"`, `"attached_ticket_ids":[]`)
	if got, _ := art.GetArtifact(ctx, asset.ID); got == nil || len(got.AttachedTicketIDs) != 0 || got.Scope != domain.ArtifactScopeProject {
		t.Fatalf("after the task's deletion = %+v", got)
	}
}
