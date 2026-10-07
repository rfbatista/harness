package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// On the wired graph, the projects context reads its projects' tasks and
// sessions: summaries count them, and the MCP delete_project tool refuses a
// project with a live session, naming it, until the session has stopped.
func TestProjectManagement_WiredGraph(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		plan     *planning.Service
		sessions ports.SessionRepository
	)
	// Without ExecutionModule: it registers Genkit flows in a process-wide
	// registry, so it cannot boot twice in one test binary.
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &plan, &sessions),
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
	task, _ := plan.CreateTicket(ctx, p.ID, "Projects screen", "", domain.TicketStatusInProgress)
	if _, err := plan.CreateTicket(ctx, p.ID, "Shipped", "", domain.TicketStatusDone); err != nil {
		t.Fatal(err)
	}
	live, err := sessions.Create(&domain.Session{ID: "s-live", ProjectID: p.ID, TicketID: task.ID, Task: "build", Status: domain.SessionRunning})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(&domain.Session{ID: "s-old", ProjectID: p.ID, Task: "old", Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}

	list, err := cat.Projects.ListProjectSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].OpenTaskCount != 1 || list[0].RunningSessionCount != 1 || list[0].LastActivityAt == nil {
		t.Fatalf("summaries on the wired graph = %+v, want 1 open task, 1 running session, an activity time", list)
	}

	s := server.NewMCPServer("test", "0.0.1", server.WithToolCapabilities(true))
	mcp.RegisterDomainTools(s, tooling.ProjectTools(cat.Projects))
	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(ctx, mcplib.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	deleteProject := func() *mcplib.CallToolResult {
		t.Helper()
		var req mcplib.CallToolRequest
		req.Params.Name = "delete_project"
		req.Params.Arguments = map[string]any{"project_id": p.ID}
		res, err := c.CallTool(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := deleteProject()
	text := toolText(res)
	if !res.IsError || !strings.HasPrefix(text, "PROJECT_HAS_RUNNING_SESSIONS:") || !strings.Contains(text, "s-live on task "+task.ID) {
		t.Fatalf("delete_project with a live session = isError %v %q, want an error starting with the code and naming s-live", res.IsError, text)
	}
	if strings.Contains(text, "s-old") {
		t.Fatalf("refusal names a finished session: %q", text)
	}
	if _, err := cat.Projects.GetProject(ctx, p.ID); err != nil {
		t.Fatalf("project gone after the refusal: %v", err)
	}

	if err := sessions.UpdateStatus(live.ID, domain.SessionStopped); err != nil {
		t.Fatal(err)
	}
	if res := deleteProject(); res.IsError {
		t.Fatalf("delete_project once the session stopped = %q", toolText(res))
	}
	if _, err := cat.Projects.GetProject(ctx, p.ID); err == nil {
		t.Fatal("project still there after delete")
	}
}

func toolText(res *mcplib.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcplib.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
