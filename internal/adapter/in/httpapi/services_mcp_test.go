package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/out/commandrunner"
	"operators-mcp/internal/adapter/out/containerrunner/containerrunnertest"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/servicerequests"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The services MCP server, called as a session over its session path: its six
// tools scoped to the session's project, over the real module with a real
// `sh -c` for command services and the fake container runtime for images.

type fakeAgents struct{ byID map[string]*domain.Agent }

func (f fakeAgents) ListAgents(context.Context) ([]*domain.Agent, error) { return nil, nil }
func (f fakeAgents) GetAgent(_ context.Context, id string) (*domain.Agent, error) {
	if a := f.byID[id]; a != nil {
		return a, nil
	}
	return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "no agent"}
}

type mcpFixture struct {
	*servicesFixture
	fake     *containerrunnertest.Fake
	sessions ports.SessionRepository
	lookup   *fakeRunWorktrees
	mcpURL   string
	// answers collects every text an MCP tool returned, for the secret check.
	answers   []string
	answersMu sync.Mutex
	// ticketID is the task the sessions made next work on.
	ticketID string
	// requests are the service requests agents file; waiter counts the
	// sessions' pending ones.
	requests *servicerequests.Service
	waiter   *fakeRequestWaiter
}

const mcpSecret = "hunter2-secret-value"

// newMCPFixture is the services MCP server with every service request
// approved at once, as the tests of the tools themselves want. The tests of the
// requests use newRequestsFixture.
func newMCPFixture(t *testing.T, settle time.Duration) *mcpFixture {
	t.Helper()
	f := newRequestsFixture(t, settle)
	f.autoApprove()
	return f
}

func newMCPFixtureBase(t *testing.T, settle time.Duration, withRequests bool) *mcpFixture {
	t.Helper()
	fake := &containerrunnertest.Fake{}
	lookup := &fakeRunWorktrees{}
	f := newServicesFixture(t, services.Options{
		RunWorktrees: lookup,
		Runners:      map[domain.ServiceKind]ports.ServiceProcessRunner{domain.ServiceKindCommand: commandrunner.Runner{}, domain.ServiceKindImage: fake},
		LookPath:     func(c string) (string, error) { return "/usr/bin/" + c, nil },
		StopGrace:    5 * time.Second,
	})
	t.Cleanup(func() {
		list, _ := f.svc.ListServices(context.Background(), f.projectID)
		if list != nil {
			for _, s := range list.Services {
				_, _ = f.svc.StopService(context.Background(), s.ID)
			}
		}
	})
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	sessions := sqlite.NewSessionRepository(db)
	agents := fakeAgents{byID: map[string]*domain.Agent{"agent-dev": {ID: "agent-dev", Name: "dev"}}}
	toolOpts := []tooling.ServicesOption{tooling.WithServicesSettleTimeout(settle)}
	var requests *servicerequests.Service
	var waiter *fakeRequestWaiter
	if withRequests {
		waiter = &fakeRequestWaiter{}
		requests = servicerequests.NewService(servicerequests.Deps{
			Services: f.svc, Store: f.requestStore, Sessions: sessions, Agents: agents, Waiter: waiter, Events: f.cat.Bus,
		})
		requests.Subscribe(f.cat.Bus)
		f.svc.Subscribe(f.cat.Bus)
		toolOpts = append(toolOpts, tooling.WithServiceRequests(requests))
		// The person's HTTP API, with the requests it answers.
		s := servicesOf(f.cat)
		s.Services, s.ServiceRequests = f.svc, requests
		api := httptest.NewServer(NewRouter(NewHandler(s)))
		t.Cleanup(api.Close)
		f.srv = api
	}
	srv := httptest.NewServer(mcpsession.ServicesHandler(tooling.SessionServicesTools(sessions, agents, f.svc, f.cat.Projects, toolOpts...)))
	t.Cleanup(srv.Close)
	return &mcpFixture{servicesFixture: f, fake: fake, sessions: sessions, lookup: lookup, mcpURL: srv.URL + mcpsession.ServicesPathPrefix, requests: requests, waiter: waiter}
}

// session saves a session of the project and repository with worktree set
// (its own dir and branch) or without.
func (f *mcpFixture) session(id, projectID, repoID string, worktree bool) string {
	f.t.Helper()
	s := &domain.Session{ID: id, ProjectID: projectID, RepositoryID: repoID, AgentID: "agent-dev", Task: "go", TicketID: f.ticketID, WorkingDir: f.t.TempDir(), Status: domain.SessionRunning}
	if worktree {
		s.WorkspaceID, s.Branch = "ws-"+id, "feat/"+id
	}
	if _, err := f.sessions.Create(s); err != nil {
		f.t.Fatal(err)
	}
	if worktree {
		f.lookup.list = append(f.lookup.list, &domain.RunWorktree{Key: "session:" + id, Kind: domain.RunWorktreeSession, Branch: s.Branch, Dir: s.WorkingDir, SessionID: id, SessionStatus: s.Status})
	}
	return s.WorkingDir
}

type mcpAgent struct {
	f *mcpFixture
	c *client.Client
}

func (f *mcpFixture) as(sessionID string) mcpAgent {
	f.t.Helper()
	trans, err := transport.NewStreamableHTTP(f.mcpURL + sessionID)
	if err != nil {
		f.t.Fatal(err)
	}
	c := client.NewClient(trans)
	f.t.Cleanup(func() { _ = trans.Close() })
	var req mcplib.InitializeRequest
	req.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0.0.1"}
	if _, err := c.Initialize(context.Background(), req); err != nil {
		f.t.Fatal(err)
	}
	return mcpAgent{f: f, c: c}
}

// call returns the tool's answer as a map, or its error message.
func (a mcpAgent) call(tool string, args map[string]any) (map[string]any, string) {
	a.f.t.Helper()
	var req mcplib.CallToolRequest
	req.Params.Name, req.Params.Arguments = tool, args
	res, err := a.c.CallTool(context.Background(), req)
	if err != nil {
		a.f.t.Fatal(err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(mcplib.TextContent); ok {
			text = tc.Text
		}
	}
	a.f.answersMu.Lock()
	a.f.answers = append(a.f.answers, text)
	a.f.answersMu.Unlock()
	if res.IsError {
		return nil, text
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		a.f.t.Fatalf("%s answered %q", tool, text)
	}
	return out, ""
}

// ok is call for an answer that must not be a refusal; it returns the service.
func (a mcpAgent) ok(tool string, args map[string]any) map[string]any {
	a.f.t.Helper()
	out, msg := a.call(tool, args)
	if msg != "" {
		a.f.t.Fatalf("%s refused: %s", tool, msg)
	}
	return out
}

func (a mcpAgent) refused(tool string, args map[string]any, want string) {
	a.f.t.Helper()
	if out, msg := a.call(tool, args); msg != want {
		a.f.t.Fatalf("%s = %v / %q, want the refusal %q", tool, out, msg, want)
	}
}

func named(name string) map[string]any { return map[string]any{"name": name} }

func (f *mcpFixture) commandIn(repoID, name, line, env string) string {
	f.t.Helper()
	in := commandInput(name)
	in["repository_id"], in["env"] = repoID, env
	in["command"] = map[string]any{"line": line, "ports": []int{8080}}
	return f.createID(in)
}

func TestServicesMCP_ListShowsViewsSortedWithEnvNamesOnly(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	f.session("sess-1", f.projectID, f.repoID, true)
	f.commandIn(f.repoID, "web", "sleep 30", "PORT=8080\nTOKEN="+mcpSecret)
	imageIn := imageInput("db")
	imageIn["image"] = map[string]any{"ref": "postgres:16", "ports": []map[string]any{{"host": 5432, "container": 5432}}}
	imageIn["env"] = "POSTGRES_PASSWORD=" + mcpSecret
	f.createID(imageIn)
	agent := f.as("sess-1")

	got := agent.ok("list_services", nil)
	if got["count"] != float64(2) {
		t.Fatalf("list = %v", got)
	}
	views := got["services"].([]any)
	db, web := views[0].(map[string]any), views[1].(map[string]any)
	if db["name"] != "db" || web["name"] != "web" {
		t.Fatalf("services are sorted by name: %v", views)
	}
	if !slices.Equal(anyStrings(web["env_names"]), []string{"PORT", "TOKEN"}) || !slices.Equal(anyStrings(web["ports"]), []string{"8080"}) ||
		web["kind"] != "command" || web["command"] != "sleep 30" || web["image"] != "" || web["repository"] != "api" || web["status"] != "stopped" || web["run"] != nil {
		t.Fatalf("web = %v", web)
	}
	if !slices.Equal(anyStrings(db["ports"]), []string{"5432->5432/tcp"}) || db["image"] != "postgres:16" || db["command"] != "" || db["repository"] != "" ||
		!slices.Equal(anyStrings(db["env_names"]), []string{"POSTGRES_PASSWORD"}) {
		t.Fatalf("db = %v", db)
	}

	// Whatever an agent does, no value of an env file reaches an answer.
	agent.ok("start_service", named("web"))
	agent.ok("service_status", named("web"))
	agent.ok("read_service_logs", map[string]any{"name": "web", "offset": 0})
	agent.ok("stop_service", named("web"))
	agent.call("start_service", named("nope"))
	for _, a := range f.answers {
		if strings.Contains(a, mcpSecret) {
			t.Fatalf("an answer holds an env value: %s", a)
		}
	}
}

func anyStrings(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

func TestServicesMCP_StartIsRecordedForTheSessionAndAnAlreadyRunningOneIsLeftAlone(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	f.ticketID = "task-77"
	f.session("sess-12345678abc", f.projectID, f.repoID, true)
	id := f.commandIn(f.repoID, "web", "sleep 30", "")
	agent := f.as("sess-12345678abc")
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)

	out := agent.ok("start_service", named("web"))
	svc := out["service"].(map[string]any)
	run := svc["run"].(map[string]any)
	if out["already_running"] != false || svc["status"] != "running" || run["started_by"] != "session sess-123 (dev)" || run["yours"] != true ||
		run["where"] != "main checkout" || run["dir"] != f.repoDir() || run["exit_code"] != nil || run["ended_at"] != "" {
		t.Fatalf("start = %v", out)
	}
	if by := f.service(id)["run"].(map[string]any)["started_by"].(map[string]any); by["kind"] != "session" || by["session_id"] != "sess-12345678abc" || by["agent"] != "dev" || by["task_id"] != "task-77" {
		t.Fatalf("the run is stored as started by the session, with its task: %v", by)
	}
	// The request's own announcements (the pending count) have no run yet.
	for seen := 0; seen < 2; {
		e := serviceChanges(t, events, id, 1)[0]
		run, ok := e["service"].(map[string]any)["run"].(map[string]any)
		if !ok {
			continue
		}
		seen++
		if by := run["started_by"].(map[string]any); by["task_id"] != "task-77" {
			t.Fatalf("the feed carries the task: %v", by)
		}
	}

	again := agent.ok("start_service", named("web"))
	if again["already_running"] != true || again["service"].(map[string]any)["run"].(map[string]any)["started_at"] != run["started_at"] {
		t.Fatalf("a running service is returned, not restarted: %v", again)
	}

	stopped := agent.ok("stop_service", named("web"))["service"].(map[string]any)
	if stopped["status"] != "stopped" || stopped["run"].(map[string]any)["ended_at"] == "" {
		t.Fatalf("stop answers once the run has ended: %v", stopped)
	}
	if unchanged := agent.ok("stop_service", named("web"))["service"].(map[string]any); unchanged["status"] != "stopped" {
		t.Fatalf("stopping a stopped service changes nothing: %v", unchanged)
	}
	if st := agent.ok("service_status", named("web"))["service"].(map[string]any); st["status"] != "stopped" {
		t.Fatalf("status = %v", st)
	}
	restarted := agent.ok("restart_service", named("web"))["service"].(map[string]any)
	if restarted["status"] != "running" || restarted["run"].(map[string]any)["started_at"] == run["started_at"] && restarted["run"].(map[string]any)["ended_at"] != "" {
		t.Fatalf("restart = %v", restarted)
	}
}

func TestServicesMCP_ARestartRecordsTheSessionsTaskAndATasklessSessionHasNone(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	f.ticketID = "task-9"
	f.session("sess-task", f.projectID, f.repoID, true)
	f.ticketID = ""
	f.session("sess-bare", f.projectID, f.repoID, true)
	id := f.commandIn(f.repoID, "web", "sleep 30", "")
	by := func() map[string]any { return f.service(id)["run"].(map[string]any)["started_by"].(map[string]any) }

	f.as("sess-task").ok("restart_service", named("web"))
	if got := by(); got["task_id"] != "task-9" || got["session_id"] != "sess-task" {
		t.Fatalf("restart by a session on a task: %v", got)
	}
	f.as("sess-bare").ok("restart_service", named("web"))
	if got := by(); got["task_id"] != "" || got["session_id"] != "sess-bare" {
		t.Fatalf("a session with no task has an empty task_id: %v", got)
	}
}

func TestServicesMCP_AFailedRunIsANormalAnswer(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	f.session("sess-1", f.projectID, f.repoID, true)
	f.commandIn(f.repoID, "web", "sleep 30", "")
	if err := os.RemoveAll(f.repoDir()); err != nil {
		t.Fatal(err)
	}

	svc := f.as("sess-1").ok("start_service", named("web"))["service"].(map[string]any)
	run := svc["run"].(map[string]any)
	if svc["status"] != "failed" || !strings.Contains(run["error"].(string), "does not exist") {
		t.Fatalf("a run that could not start answers failed with its error: %v", svc)
	}
}

func TestServicesMCP_AStartStillStartingAfterTheTimeLimitIsAnsweredStarting(t *testing.T) {
	f := newMCPFixture(t, 150*time.Millisecond)
	f.session("sess-1", f.projectID, f.repoID, true)
	f.fake.Hold = true
	in := imageInput("db")
	in["repository_id"] = f.repoID
	f.createID(in)
	agent := f.as("sess-1")

	began := time.Now()
	out := agent.ok("start_service", named("db"))
	if svc := out["service"].(map[string]any); svc["status"] != "starting" || out["already_running"] != false {
		t.Fatalf("a pull that outlasts the limit is a normal answer, starting: %v", out)
	}
	if time.Since(began) < 100*time.Millisecond || time.Since(began) > 3*time.Second {
		t.Fatalf("waited %v for the limit", time.Since(began))
	}

	// The run goes on: once the container is up, the status says running.
	f.fake.Runs()[0].Release()
	f.awaitStatus(f.createdID("db"), "running")
	if svc := agent.ok("service_status", named("db"))["service"].(map[string]any); svc["status"] != "running" || svc["run"].(map[string]any)["where"] != "container" {
		t.Fatalf("status = %v", svc)
	}
	restarted := agent.ok("restart_service", named("db"))["service"].(map[string]any)
	if restarted["status"] != "starting" && restarted["status"] != "running" {
		t.Fatalf("restart = %v", restarted)
	}
}

// createdID finds a service's id by its name.
func (f *servicesFixture) createdID(name string) string {
	f.t.Helper()
	list, err := f.svc.ListServices(context.Background(), f.projectID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, s := range list.Services {
		if s.Name == name {
			return s.ID
		}
	}
	f.t.Fatalf("no service %q", name)
	return ""
}

func TestServicesMCP_OwnWorktreeRunsInTheSessionsWorktreeAndTheWebRestartRepeatsIt(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	dir := f.session("sess-1", f.projectID, f.repoID, true)
	f.session("sess-2", f.projectID, f.repoID, true)
	id := f.commandIn(f.repoID, "web", "pwd; sleep 30", "")
	mine, other := f.as("sess-1"), f.as("sess-2")

	run := mine.ok("start_service", map[string]any{"name": "web", "worktree": "own"})["service"].(map[string]any)["run"].(map[string]any)
	if run["where"] != "your worktree" || run["dir"] != dir || run["yours"] != true || run["started_by"] != "session sess-1 (dev)" {
		t.Fatalf("run = %v", run)
	}
	f.awaitLog(id, dir)
	if w := f.service(id)["run"].(map[string]any)["where"].(map[string]any); w["kind"] != "session" || w["session_id"] != "sess-1" || w["branch"] != "feat/sess-1" {
		t.Fatalf("where = %v", w)
	}

	// Another session sees whose worktree it is, and that it is not theirs.
	seen := other.ok("service_status", named("web"))["service"].(map[string]any)["run"].(map[string]any)
	if seen["where"] != "worktree of session sess-1" || seen["yours"] != false || seen["started_by"] != "session sess-1 (dev)" {
		t.Fatalf("another session sees %v", seen)
	}

	// The web UI's restart repeats the last run: the same worktree, now
	// started by the person (ADR 0077).
	if code, out := f.operate("restart_service", id); code != 200 {
		t.Fatalf("restart = %d %v", code, out)
	}
	web := f.awaitStatus(id, "running")["run"].(map[string]any)
	if w := web["where"].(map[string]any); w["kind"] != "session" || w["dir"] != dir || web["worktree_key"] != "session:sess-1" || web["started_by"].(map[string]any)["kind"] != "person" {
		t.Fatalf("a web restart repeats the worktree, started by the person: %v", web)
	}

	// A fresh start begins in the main checkout.
	f.operate("stop_service", id)
	f.operate("start_service", id)
	if w := f.awaitStatus(id, "running")["run"].(map[string]any)["where"].(map[string]any); w["kind"] != "main" || w["dir"] != f.repoDir() {
		t.Fatalf("a web start runs in the main checkout: %v", w)
	}

	// An agent's restart with no worktree repeats the last run too.
	repeated := mine.ok("restart_service", named("web"))["service"].(map[string]any)["run"].(map[string]any)
	if repeated["where"] != "main checkout" {
		t.Fatalf("restart without a worktree repeats the main checkout run: %v", repeated)
	}

	// restart_service takes worktree as start does, and moves it into the worktree again.
	back := mine.ok("restart_service", map[string]any{"name": "web", "worktree": "own"})["service"].(map[string]any)["run"].(map[string]any)
	if back["where"] != "your worktree" || back["dir"] != dir {
		t.Fatalf("restart own = %v", back)
	}
}

func TestServicesMCP_OwnWorktreeRefusals(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	otherRepo, err := f.cat.Projects.CreateRepository(context.Background(), f.projectID, "docs", "", "https://example.com/docs.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.session("sess-1", f.projectID, f.repoID, true)
	f.session("sess-bare", f.projectID, f.repoID, false)
	f.session("sess-docs", f.projectID, otherRepo.ID, true)
	f.commandIn(f.repoID, "web", "sleep 30", "")
	noRepo := commandInput("job")
	f.createID(noRepo)
	img := imageInput("db")
	img["repository_id"] = f.repoID
	f.createID(img)

	own := func(name string) map[string]any { return map[string]any{"name": name, "worktree": "own"} }
	mine := f.as("sess-1")
	mine.refused("start_service", own("db"), `"db" cannot run in your worktree: it is an image service`)
	mine.refused("start_service", own("job"), `"job" cannot run in your worktree: it has no repository`)
	mine.refused("restart_service", own("job"), `"job" cannot run in your worktree: it has no repository`)
	f.as("sess-docs").refused("start_service", own("web"), `"web" cannot run in your worktree: it runs from repository api, not yours`)
	f.as("sess-bare").refused("start_service", own("web"), `"web" cannot run in your worktree: this session has no worktree`)
	mine.refused("start_service", map[string]any{"name": "web", "worktree": "main"}, `worktree must be "own" or omitted`)
	mine.refused("restart_service", map[string]any{"name": "web", "worktree": "main"}, `worktree must be "own" or omitted`)
	mine.refused("start_service", map[string]any{"name": "web", "worktree": ""}, `worktree must be "own" or omitted`)
	mine.refused("restart_service", map[string]any{"name": "web", "worktree": ""}, `worktree must be "own" or omitted`)
	for _, name := range []string{"web", "job", "db"} {
		if st := f.status(f.createdID(name)); st != "stopped" {
			t.Fatalf("a refused start starts nothing: %s is %s", name, st)
		}
	}
}

func TestServicesMCP_ReadServiceLogs(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	f.session("sess-1", f.projectID, f.repoID, true)
	id := f.commandIn(f.repoID, "web", `i=1; while [ $i -le 300 ]; do echo line-$i; i=$((i+1)); done; head -c 100000 /dev/zero | tr '\0' 'x'; echo; echo end-$((40+2)); sleep 30`, "")
	agent := f.as("sess-1")
	agent.ok("start_service", named("web"))
	f.awaitLog(id, "\nend-42\n")

	tail := agent.ok("read_service_logs", map[string]any{"name": "web", "tail_lines": 2})
	if strings.Count(tail["text"].(string), "\n") != 2 || !strings.HasSuffix(tail["text"].(string), "end-42\n") {
		t.Fatalf("tail_lines 2 = %q", tail["text"])
	}
	def := agent.ok("read_service_logs", named("web"))
	if strings.Count(def["text"].(string), "\n") != 200 || def["truncated"] != false {
		t.Fatalf("the default is 200 lines; older lines existing is not a cut: %d lines, truncated %v", strings.Count(def["text"].(string), "\n"), def["truncated"])
	}

	first := agent.ok("read_service_logs", map[string]any{"name": "web", "offset": 0})
	text := first["text"].(string)
	if len(text) != 64<<10 || first["truncated"] != true || first["next_offset"] != float64(64<<10) || first["first_offset"] != float64(0) {
		t.Fatalf("from an offset at most 64 KB come back: %d bytes, %v", len(text), first)
	}
	// Following with next_offset reaches the end, which is not truncated.
	var all strings.Builder
	all.WriteString(text)
	next := first["next_offset"]
	for i := 0; i < 5; i++ {
		page := agent.ok("read_service_logs", map[string]any{"name": "web", "offset": next})
		all.WriteString(page["text"].(string))
		if page["truncated"] != true {
			next = page["next_offset"]
			break
		}
		next = page["next_offset"]
	}
	if !strings.Contains(all.String(), "line-300\n") || !strings.HasSuffix(all.String(), "end-42\n") || next != float64(len(all.String())) {
		t.Fatalf("following reads the whole log: next %v of %d", next, all.Len())
	}

	for _, bad := range []any{0, -1, 2001, 1.5} {
		agent.refused("read_service_logs", map[string]any{"name": "web", "tail_lines": bad}, "tail_lines must be between 1 and 2000")
	}
}

func TestServicesMCP_ATailThatTheWindowCutsIsTruncatedAndStartsOnWholeLines(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	f.session("sess-1", f.projectID, f.repoID, true)
	// 300 KB of one line, then two short ones: the 256 KB tail window starts
	// inside the long line.
	id := f.commandIn(f.repoID, "web", `head -c 300000 /dev/zero | tr '\0' 'x'; echo; echo mid; echo end-42; sleep 30`, "")
	agent := f.as("sess-1")
	agent.ok("start_service", named("web"))
	f.awaitLog(id, "\nend-42\n")

	few := agent.ok("read_service_logs", map[string]any{"name": "web", "tail_lines": 2})
	if few["text"] != "mid\nend-42\n" || few["truncated"] != false {
		t.Fatalf("two lines are answered whole, not cut: %q truncated %v", few["text"], few["truncated"])
	}
	many := agent.ok("read_service_logs", map[string]any{"name": "web", "tail_lines": 50})
	if many["text"] != "mid\nend-42\n" || many["truncated"] != true {
		t.Fatalf("the window cut the answer: no partial first line, truncated: %q %v", many["text"], many["truncated"])
	}
	if from := agent.ok("read_service_logs", map[string]any{"name": "web", "offset": 0}); from["truncated"] != true {
		t.Fatalf("a 64 KB window cuts: %v", from["truncated"])
	}
}

func TestServicesMCP_RefusalsCarryTheContractsMessages(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	f.session("sess-1", f.projectID, f.repoID, true)
	f.session("sess-orphan", "", "", false)
	f.commandIn(f.repoID, "web", "sleep 30", "")
	f.createID(imageInput("db"))
	agent := f.as("sess-1")

	for _, tool := range serviceTakingTools() {
		agent.refused(tool, named("ghost"), `no service named "ghost" in this project; call list_services`)
	}
	f.as("sess-orphan").refused("list_services", nil, "this session has no project")
	f.as("sess-orphan").refused("start_service", named("web"), "this session has no project")
	f.as("ghost-session").refused("list_services", nil, "session not found")
}

func TestServicesMCP_ASessionSeesOnlyItsProjectsServices(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	other, err := f.cat.Projects.CreateProject(context.Background(), "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.session("sess-1", f.projectID, f.repoID, true)
	f.session("sess-2", other.ID, "", false)
	f.commandIn(f.repoID, "web", "sleep 30", "")
	if _, err := f.svc.CreateService(context.Background(), other.ID, domain.ServiceInput{
		Name: "secret-job", Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "sleep 30"},
	}); err != nil {
		t.Fatal(err)
	}

	mine, theirs := f.as("sess-1"), f.as("sess-2")
	got := mine.ok("list_services", nil)
	if got["count"] != float64(1) || got["services"].([]any)[0].(map[string]any)["name"] != "web" {
		t.Fatalf("project 1 lists %v", got)
	}
	for _, tool := range serviceTakingTools() {
		mine.refused(tool, named("secret-job"), `no service named "secret-job" in this project; call list_services`)
		theirs.refused(tool, named("web"), `no service named "web" in this project; call list_services`)
	}
	if got := theirs.ok("list_services", nil); got["count"] != float64(1) || got["services"].([]any)[0].(map[string]any)["name"] != "secret-job" {
		t.Fatalf("project 2 lists %v", got)
	}
}

func TestServicesMCP_TheContainerRuntimeAndSandboxRefusalsUseTheContractsMessages(t *testing.T) {
	// No runtime command on the server's PATH.
	fake := &containerrunnertest.Fake{}
	f := newServicesFixture(t, services.Options{
		Runners:  map[domain.ServiceKind]ports.ServiceProcessRunner{domain.ServiceKindCommand: commandrunner.Runner{}, domain.ServiceKindImage: fake},
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Sandbox:  fakeSandboxSwitch(true),
	})
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	sessions := sqlite.NewSessionRepository(db)
	srv := httptest.NewServer(mcpsession.ServicesHandler(tooling.SessionServicesTools(sessions, nil, f.svc, f.cat.Projects)))
	t.Cleanup(srv.Close)
	m := &mcpFixture{servicesFixture: f, fake: fake, sessions: sessions, lookup: &fakeRunWorktrees{}, mcpURL: srv.URL + mcpsession.ServicesPathPrefix}
	m.session("sess-1", f.projectID, f.repoID, true)
	f.createID(imageInput("db"))
	f.createID(commandInput("web"))
	agent := m.as("sess-1")

	agent.refused("start_service", named("db"), `the container runtime "docker" was not found on the harness server; image services cannot start`)
	if code, out := f.call("POST", "/api/set_services_settings", map[string]any{"container_runtime": "podman"}); code != 200 {
		t.Fatalf("set runtime = %d %v", code, out)
	}
	agent.refused("restart_service", named("db"), `the container runtime "podman" was not found on the harness server; image services cannot start`)
	// The sandbox is on and this platform's driver is absent (the fixture has none).
	agent.refused("start_service", named("web"), "the sandbox is on but this platform has no sandbox driver; command services cannot start")
	agent.refused("restart_service", named("web"), "the sandbox is on but this platform has no sandbox driver; command services cannot start")
}

// serviceTakingTools are the tools that name an existing service: every tool
// but the list and create_service.
func serviceTakingTools() []string {
	var out []string
	for _, name := range tooling.SessionServicesToolNames[1:] {
		if name != "create_service" {
			out = append(out, name)
		}
	}
	return out
}
