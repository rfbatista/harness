package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// wiredResources boots the graph without ExecutionModule (Genkit registers
// flows process-wide) and returns a task to add things to.
func wiredResources(t *testing.T) (*planning.Service, *orchestration.Service, ports.SessionRepository, *domain.Ticket) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		plan     *planning.Service
		orch     *orchestration.Service
		sessions ports.SessionRepository
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &plan, &orch, &sessions),
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
	tk, err := plan.CreateTicket(ctx, p.ID, "Task", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	return plan, orch, sessions, tk
}

// On the wired graph an upload keeps its bytes outside any worktree, names
// its content route, goes on the project feed, and goes with its task.
func TestTaskResources_WiredGraph(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	plan, orch, _, tk := wiredResources(t)
	feed, err := orch.FollowProject(ctx, tk.ProjectID)
	if err != nil {
		t.Fatal(err)
	}

	r, err := plan.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: tk.ID, FileName: "notes.md", Content: strings.NewReader("# Notes")})
	if err != nil {
		t.Fatal(err)
	}
	if r.ContentURL != "/api/ticket_resources/"+r.ID+"/content" || r.Kind != domain.ResourceFile {
		t.Fatalf("resource = %+v", r)
	}
	path, err := plan.TicketResourcePath(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "# Notes" || !strings.Contains(path, "harness-task-resources-") {
		t.Fatalf("bytes at %s = %q, %v", path, b, err)
	}
	c, err := plan.AddTicketComment(ctx, tk.ID, "look at the notes")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(got) < 2 {
		select {
		case ch := <-feed:
			switch {
			case ch.TicketResource != nil:
				got = append(got, "resource "+ch.TicketResource.Resource.ID)
			case ch.TicketComment != nil:
				got = append(got, "comment "+ch.TicketComment.Comment.ID)
			}
		case <-ctx.Done():
			t.Fatalf("feed carried %v", got)
		}
	}
	if got[0] != "resource "+r.ID || got[1] != "comment "+c.ID {
		t.Fatalf("feed = %v", got)
	}

	if err := plan.DeleteTicket(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("bytes outlived their task: %v", err)
	}
}

