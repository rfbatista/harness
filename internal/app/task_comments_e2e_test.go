package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// TestSessionComments_EndToEnd drives an agent's comment through the
// session's own MCP endpoint, as the server wires it, and follows it to the
// project feed, the person's routes, the task page's seed and the sessions'
// turns: a session's comment wakes no one, the person's still does.
func TestSessionComments_EndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		plan     *planning.Service
		orch     *orchestration.Service
		sessions ports.SessionRepository
		channel  *taskchannel.Service
		art      *artifacts.Service
	)
	fxApp := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &plan, &orch, &sessions, &channel, &art),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })

	p, err := cat.Projects.CreateProject(ctx, "harness", dir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(ctx, p.ID, "Task", "", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := cat.Agents.CreateAgent(ctx, "comment-e2e-agent", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*domain.Session{{ID: "s-writer", AgentID: agent.ID}, {ID: "s-other"}} {
		s.ProjectID, s.TicketID, s.Task, s.Status, s.Interactive, s.RunsOn = p.ID, tk.ID, "work", domain.SessionRunning, true, domain.RunnerTUI
		if _, err := sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}

	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Sessions: orch, Planning: plan, TaskComments: plan,
	})))
	mux.Handle(mcpsession.PathPrefix, mcpsession.TaskHandler(sessionTaskTools(cat, orch, plan, sessions, art, channel, nil)))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, Comments: plan}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	feed := followFeed(t, srv.URL, p.ID)

	writer := dialTask(t, srv.URL+mcpsession.PathPrefix+"s-writer")
	other := dialTask(t, srv.URL+mcpsession.PathPrefix+"s-other")

	// The agent comments as itself; the feed carries who wrote it.
	added := callTask(t, writer, "add_task_comment", map[string]any{"body": "Plan approved; starting on the server."})
	var out struct {
		Comment domain.TicketComment `json:"comment"`
	}
	if err := json.Unmarshal([]byte(added), &out); err != nil {
		t.Fatalf("add_task_comment = %s", added)
	}
	c := out.Comment
	if c.Author != domain.CommentBySession || c.SessionID != "s-writer" || c.AgentName != "comment-e2e-agent" {
		t.Fatalf("comment = %+v", c)
	}
	feed.saw(t, "ticket_comment", c.ID, `"author":"session"`, `"session_id":"s-writer"`, `"agent_name":"comment-e2e-agent"`)

	// Another session can neither rewrite nor delete it. The MCP bridge hands
	// the agent a structured error's message, not its code.
	const notYours = "only the session that wrote this comment can change it"
	if got := callTaskErr(t, other, "update_task_comment", map[string]any{"comment_id": c.ID, "body": "mine now"}); got != notYours {
		t.Fatalf("another session's update = %s", got)
	}
	if got := callTaskErr(t, other, "delete_task_comment", map[string]any{"comment_id": c.ID}); got != notYours {
		t.Fatalf("another session's delete = %s", got)
	}

	// The person's comment wakes both sessions; the agent's woke neither.
	status, body := postJSON(t, srv.URL+"/api/add_ticket_comment", `{"ticket_id":"`+tk.ID+`","body":"Thanks, go ahead."}`)
	if status != 200 {
		t.Fatalf("person's add = %d %s", status, body)
	}
	for _, id := range []string{"s-writer", "s-other"} {
		turn := nextTurn(t, orch, id)
		if !strings.HasPrefix(turn, "[task update · comment] The person commented") || !strings.Contains(turn, "Thanks, go ahead.") || strings.Contains(turn, c.ID) {
			t.Fatalf("%s's turn = %q", id, turn)
		}
		if again, _ := orch.TurnEnded(ctx, id, false); again != "" {
			t.Fatalf("%s got another turn: %q", id, again)
		}
	}

	// The person's routes and the task page carry the author.
	res, err := http.Get(srv.URL + "/api/list_ticket_comments?ticket_id=" + tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(list), `"author":"session"`) || !strings.Contains(string(list), `"author":"person"`) {
		t.Fatalf("list = %s", list)
	}
	res, err = http.Get(srv.URL + "/projects/" + p.ID + "/tasks/" + tk.ID + "/resources")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(page), `"author":"session","session_id":"s-writer","agent_name":"comment-e2e-agent"`) ||
		!strings.Contains(string(page), `"author":"person","session_id":"","agent_name":""`) {
		t.Fatalf("the task page's seed lacks the comments' authors: %d", res.StatusCode)
	}
	if status, body := postJSON(t, srv.URL+"/api/update_ticket_comment", `{"comment_id":"`+c.ID+`","body":"rewritten"}`); status != 403 || !strings.Contains(body, "COMMENT_NOT_YOURS") {
		t.Fatalf("person's update of the agent's comment = %d %s", status, body)
	}

	// The writer edits its own; the person deletes it.
	edited := callTask(t, writer, "update_task_comment", map[string]any{"comment_id": c.ID, "body": "Plan approved; server half done."})
	if !strings.Contains(edited, "server half done") {
		t.Fatalf("update_task_comment = %s", edited)
	}
	feed.saw(t, "ticket_comment", c.ID, "server half done")
	if status, body := postJSON(t, srv.URL+"/api/delete_ticket_comment", `{"comment_id":"`+c.ID+`"}`); status != 200 {
		t.Fatalf("person's delete of the agent's comment = %d %s", status, body)
	}
	feed.saw(t, "ticket_comment", c.ID)
	if got := callTaskErr(t, writer, "delete_task_comment", map[string]any{"comment_id": c.ID}); got != "comment not found" {
		t.Fatalf("delete after the person's = %s", got)
	}
}

// projectFeed is what /api/events said for a project, as it arrives.
type projectFeed struct {
	mu      sync.Mutex
	changes []map[string]json.RawMessage
}

func followFeed(t *testing.T, base, projectID string) *projectFeed {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/events?project_id="+projectID, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	f := &projectFeed{}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var change map[string]json.RawMessage
			if json.Unmarshal([]byte(data), &change) == nil {
				f.mu.Lock()
				f.changes = append(f.changes, change)
				f.mu.Unlock()
			}
		}
	}()
	return f
}

// saw waits for a change under key whose object holds every fragment.
func (f *projectFeed) saw(t *testing.T, key string, fragments ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, c := range f.changes {
			if raw, ok := c[key]; ok && containsAll(string(raw), fragments) {
				f.mu.Unlock()
				return
			}
		}
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %q change with %q on the feed", key, fragments)
}

func dialTask(t *testing.T, url string) *client.Client {
	t.Helper()
	trans, err := transport.NewStreamableHTTP(url)
	if err != nil {
		t.Fatal(err)
	}
	c := client.NewClient(trans)
	t.Cleanup(func() { _ = trans.Close() })
	var req mcplib.InitializeRequest
	req.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcplib.Implementation{Name: "e2e", Version: "0.0.1"}
	if _, err := c.Initialize(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	return c
}

// callTaskRaw calls a task tool over MCP and answers its text and whether it
// was an error.
func callTaskRaw(t *testing.T, c *client.Client, name string, args map[string]any) (string, bool) {
	t.Helper()
	var call mcplib.CallToolRequest
	call.Params.Name, call.Params.Arguments = name, args
	res, err := c.CallTool(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, part := range res.Content {
		if tc, ok := part.(mcplib.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

func callTask(t *testing.T, c *client.Client, name string, args map[string]any) string {
	t.Helper()
	text, isErr := callTaskRaw(t, c, name, args)
	if isErr {
		t.Fatalf("%s failed: %s", name, text)
	}
	return text
}

func callTaskErr(t *testing.T, c *client.Client, name string, args map[string]any) string {
	t.Helper()
	text, isErr := callTaskRaw(t, c, name, args)
	if !isErr {
		t.Fatalf("%s succeeded, want an error: %s", name, text)
	}
	return text
}

func postJSON(t *testing.T, url, body string) (int, string) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// nextTurn calls the session's Stop hook until it hands back a turn.
func nextTurn(t *testing.T, orch *orchestration.Service, id string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		next, err := orch.TurnEnded(context.Background(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		if next != "" {
			return next
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s got no turn", id)
	return ""
}
