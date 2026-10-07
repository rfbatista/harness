//go:build !windows

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// archE2E is the server wired as it runs, with a shell standing in for claude
// on the terminal host, the HTTP API on a test server, and the task tools as
// the per-session MCP endpoint serves them.
type archE2E struct {
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

	mu   sync.Mutex
	feed []map[string]json.RawMessage
}

func newArchE2E(t *testing.T) *archE2E {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	// The stand-in claude keeps its terminal open and echoes what is typed.
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &archE2E{t: t, ctx: context.Background()}
	var (
		cat      catalog.Catalog
		sessions ports.SessionRepository
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, ExecutionModule,
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
	tk, err := e.plan.CreateTicket(e.ctx, p.ID, "Architect highlights", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	e.project, e.repo, e.ticket = p.ID, r.ID, tk.ID

	e.srv = httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Sessions: e.orch, Planning: e.plan, TaskChannel: e.channel})))
	t.Cleanup(e.srv.Close)
	e.tools = map[string]domain.Tool{}
	for _, tool := range tooling.SessionTaskTools(e.plan, sessions, cat.Agents, cat.Architecture,
		tooling.PeerStarter{Sessions: e.orch, Repositories: cat.Projects}, tooling.ArtifactTooling{}, e.channel) {
		e.tools[tool.Name] = tool
	}
	e.follow()
	return e
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=e2e@test", "-c", "user.name=e2e", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// follow reads the project feed (/api/events) for the whole test.
func (e *archE2E) follow() {
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events?project_id="+e.project, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
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
				e.mu.Lock()
				e.feed = append(e.feed, change)
				e.mu.Unlock()
			}
		}
	}()
}

// sawOnFeed waits for a feed change under key whose object contains every
// fragment.
func (e *archE2E) sawOnFeed(key string, fragments ...string) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		for _, c := range e.feed {
			if raw, ok := c[key]; ok && containsAll(string(raw), fragments) {
				e.mu.Unlock()
				return
			}
		}
		e.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("no %q change with %q on the feed", key, fragments)
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// tool calls a task tool as session id, the way the per-session MCP endpoint
// does, and returns its result as JSON.
func (e *archE2E) tool(id, name string, args map[string]any) (map[string]any, error) {
	e.t.Helper()
	tool, ok := e.tools[name]
	if !ok {
		e.t.Fatalf("no tool %s", name)
	}
	res, err := tool.Handler(tooling.WithSessionID(e.ctx, id), args)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out, nil
}

func (e *archE2E) mustTool(id, name string, args map[string]any) map[string]any {
	e.t.Helper()
	out, err := e.tool(id, name, args)
	if err != nil {
		e.t.Fatalf("%s as %s: %v", name, id, err)
	}
	return out
}

func (e *archE2E) http(method, path, body string) (int, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

// stop is the session's Stop hook, as the CLI posts it; it returns the
// reason claude is told to go on with ("" when it may stop).
func (e *archE2E) stop(id string) string {
	e.t.Helper()
	status, out := e.http("POST", httpapi.InteractiveSessionHookPath+"?session_id="+id+"&event=Stop", `{"hook_event_name":"Stop","stop_hook_active":false}`)
	if status != 200 {
		e.t.Fatalf("Stop hook: %d", status)
	}
	if out == nil {
		return ""
	}
	if out["decision"] != "block" {
		e.t.Fatalf("Stop hook answer: %+v", out)
	}
	return out["reason"].(string)
}

// TestArchitectChannel_EndToEnd drives the whole channel through the real
// task tools, the HTTP API, the Stop hook and the project feed.
func TestArchitectChannel_EndToEnd(t *testing.T) {
	e := newArchE2E(t)

	// The person starts an architect in their terminal client.
	arch, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.ticket, Mode: "architect"})
	if err != nil {
		t.Fatal(err)
	}
	if arch.Role != domain.RoleArchitect || arch.ArchitectSessionID == nil || *arch.ArchitectSessionID != arch.ID {
		t.Fatalf("architect: %+v", arch)
	}

	// It delegates with start_task_session: the delegate runs on the server
	// and gets a status-check loop.
	started := e.mustTool(arch.ID, "start_task_session", map[string]any{"prompt": "Plan the server side.", "status_check_minutes": float64(5)})
	dev, _ := started["session_id"].(string)
	check, _ := started["status_check"].(map[string]any)
	if dev == "" || check == nil || check["every_minutes"] != float64(5) || check["state"] != "active" {
		t.Fatalf("start_task_session: %+v", started)
	}
	e.sawOnFeed("status_check", `"delegate_session_id":"`+dev+`"`, `"state":"active"`)
	e.sawOnFeed("session", `"id":"`+dev+`"`, `"role":"delegate"`)

	// The delegate reports. The architect is mid-turn, so the report waits
	// for its Stop hook, which hands it back as the turn to go on with.
	sent := e.mustTool(dev, "message_architect", map[string]any{"kind": "status_report", "status": "working", "body": "Halfway through the plan."})
	if sent["delivered"] != false {
		t.Fatalf("a busy architect gets it later: %+v", sent)
	}
	reason := e.stop(arch.ID)
	if !strings.HasPrefix(reason, "[task message ") || !strings.Contains(reason, "· status_report/working · from ") || !strings.Contains(reason, "Halfway through the plan.") {
		t.Fatalf("the architect's Stop hook: %q", reason)
	}
	e.sawOnFeed("task_message", `"delivered":true`, `"status":"working"`)
	if _, out := e.http("GET", "/api/task_messages?ticket_id="+e.ticket, ""); len(out["messages"].([]any)) != 1 {
		t.Fatalf("task_messages: %+v", out)
	}

	// Only the architect moves the task.
	_, err = e.tool(dev, "update_task_status", map[string]any{"status": "review"})
	if errs.Code(err) != "TASK_STATUS_OWNED_BY_ARCHITECT" {
		t.Fatalf("a delegate's move is refused: %v", err)
	}
	e.mustTool(arch.ID, "update_task_status", map[string]any{"status": "in_progress", "reason": "delegated the server plan"})
	e.sawOnFeed("task_status", `"status":"in_progress"`, `"by":"session"`, `"by_session_id":"`+arch.ID+`"`, `"reason":"delegated the server plan"`)

	// The architect asks the person for a review; the person answers in the
	// UI; the answer reaches the architect at its next Stop.
	e.mustTool(arch.ID, "request_user_review", map[string]any{"subject": "Server plan", "body": "Please look at the plan.", "about_session_id": dev})
	e.sawOnFeed("ticket", `"pending_reviews":1`, `"architect_session_id":"`+arch.ID+`"`)
	_, inbox := e.http("GET", "/api/review_requests?project_id="+e.project+"&state=pending", "")
	reviews := inbox["review_requests"].([]any)
	if len(reviews) != 1 {
		t.Fatalf("inbox: %+v", inbox)
	}
	reviewID := reviews[0].(map[string]any)["id"].(string)
	status, answered := e.http("POST", "/api/respond_review_request", `{"review_id":"`+reviewID+`","decision":"changes_requested","note":"Split step 6."}`)
	if status != 200 || answered["delivered"] != false {
		t.Fatalf("respond: %d %+v", status, answered)
	}
	if reason := e.stop(arch.ID); reason != "[review response "+reviewID+" · review_response · changes_requested]\nSubject: Server plan\nSplit step 6." {
		t.Fatalf("review response turn: %q", reason)
	}
	if status, _ := e.http("POST", "/api/respond_review_request", `{"review_id":"`+reviewID+`","decision":"approved"}`); status != 409 {
		t.Fatalf("a settled review: %d", status)
	}
	if reason := e.stop(arch.ID); reason != "" {
		t.Fatalf("nothing waits: the architect stops, got %q", reason)
	}

	// The architect replies; the delegate's terminal is idle, so the reply is
	// typed into it at once.
	if reason := e.stop(dev); reason != "" {
		t.Fatalf("the delegate's first turn ends: %q", reason)
	}
	reply := e.mustTool(arch.ID, "reply_to_session", map[string]any{"session_id": dev, "body": "Go ahead with the courier."})
	if reply["delivered"] != true {
		t.Fatalf("an idle delegate takes the reply at once: %+v", reply)
	}
	mine := e.mustTool(dev, "list_task_messages", map[string]any{})
	if mine["count"] != float64(2) {
		t.Fatalf("the delegate sees its own two messages: %+v", mine)
	}

	// A status check comes due; the architect is idle but in a client
	// terminal, so it waits for the next Stop.
	e.channel.SetClock(func() time.Time { return time.Now().Add(6 * time.Minute) })
	e.channel.FireDue()
	if reason := e.stop(arch.ID); !strings.HasPrefix(reason, "[status check · ") || !strings.Contains(reason, "session "+dev) {
		t.Fatalf("status check turn: %q", reason)
	}
	e.sawOnFeed("status_check", `"delivered":true`, `"fired_count":1`)

	// The person pauses the loop.
	if status, out := e.http("POST", "/api/set_status_check", `{"delegate_session_id":"`+dev+`","every_minutes":0}`); status != 200 || out["status_check"].(map[string]any)["state"] != "paused" {
		t.Fatalf("pause: %d %+v", status, out)
	}

	// Session and task JSON carry the role fields.
	_, got := e.http("GET", "/api/sessions/"+dev, "")
	sess, _ := got["session"].(map[string]any)
	if sess == nil {
		sess = got
	}
	if sess["role"] != "delegate" || sess["architect_session_id"] != arch.ID || sess["status_check"] == nil {
		t.Fatalf("delegate JSON: %+v", got)
	}
	_, gotTicket := e.http("GET", "/api/get_ticket?ticket_id="+e.ticket, "")
	tk := gotTicket["ticket"].(map[string]any)
	if tk["architect_session_id"] != arch.ID || tk["pending_reviews"] != float64(0) || tk["status"] != "in_progress" {
		t.Fatalf("ticket JSON: %+v", tk)
	}

	// The delegate ends: its loop ends, and the architect is told how.
	if err := e.orch.Stop(e.ctx, dev); err != nil {
		t.Fatal(err)
	}
	e.sawOnFeed("status_check", `"delegate_session_id":"`+dev+`"`, `"state":"ended"`)
	if reason := e.stop(arch.ID); !strings.Contains(reason, "· status ended: stopped ·") {
		t.Fatalf("last status check: %q", reason)
	}
}
