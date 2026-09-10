package mcpsession

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
)

// newTaskServer serves the task tools over HTTP against an in-memory database
// holding one project, one ticket, and a session spawned into it.
func newTaskServer(t *testing.T) (baseURL, ticketID string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)

	proj, err := projects.Create("p", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(proj.ID, "Ship the thing", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(&domain.Session{
		ID: "sess-1", ProjectID: proj.ID, TicketID: tk.ID, Task: "go", Status: domain.SessionRunning,
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(TaskHandler(tooling.SessionTaskTools(plan, sessions)))
	t.Cleanup(srv.Close)
	return srv.URL, tk.ID
}

func dial(t *testing.T, url string) *client.Client {
	t.Helper()
	trans, err := transport.NewStreamableHTTP(url)
	if err != nil {
		t.Fatal(err)
	}
	c := client.NewClient(trans)
	t.Cleanup(func() { _ = trans.Close() })

	var req mcplib.InitializeRequest
	req.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0.0.1"}
	if _, err := c.Initialize(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	return c
}

func textOf(t *testing.T, res *mcplib.CallToolResult) string {
	t.Helper()
	for _, c := range res.Content {
		if tc, ok := c.(mcplib.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no text content in tool result")
	return ""
}

// The session id comes from the request path, so a client that only knows its
// own URL gets its own task — and can write a document onto it.
func TestTaskHandler_ScopesBySessionInPath(t *testing.T) {
	baseURL, _ := newTaskServer(t)
	c := dial(t, baseURL+PathPrefix+"sess-1")
	ctx := context.Background()

	listed, err := c.ListTools(ctx, mcplib.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != len(tooling.SessionTaskToolNames) {
		t.Fatalf("served %d tools, want %d", len(listed.Tools), len(tooling.SessionTaskToolNames))
	}

	var call mcplib.CallToolRequest
	call.Params.Name = "create_task_document"
	call.Params.Arguments = map[string]any{"title": "Plan", "content": "step one"}
	res, err := c.CallTool(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("create failed: %s", textOf(t, res))
	}

	call = mcplib.CallToolRequest{}
	call.Params.Name = "get_task"
	res, err = c.CallTool(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	txt := textOf(t, res)
	if !strings.Contains(txt, "Ship the thing") || !strings.Contains(txt, "Plan") {
		t.Fatalf("get_task did not report the task and its new document: %s", txt)
	}
}

// A path that names no known session resolves to nothing, so the tools refuse.
func TestTaskHandler_UnknownSession(t *testing.T) {
	baseURL, _ := newTaskServer(t)
	c := dial(t, baseURL+PathPrefix+"ghost")

	var call mcplib.CallToolRequest
	call.Params.Name = "get_task"
	res, err := c.CallTool(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(textOf(t, res), "session not found") {
		t.Fatalf("want a session-not-found tool error, got %+v", res)
	}
}
