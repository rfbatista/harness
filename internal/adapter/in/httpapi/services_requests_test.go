package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// Service requests (ADR 0083): an agent's changing tool files a request and
// blocks until the person answers it over HTTP.

// fakeRequestWaiter counts a session's pending service requests, as the
// orchestration does.
type fakeRequestWaiter struct {
	mu      sync.Mutex
	pending map[string]int
}

func (w *fakeRequestWaiter) ServiceRequestFiled(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending == nil {
		w.pending = map[string]int{}
	}
	w.pending[id]++
}

func (w *fakeRequestWaiter) ServiceRequestSettled(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending[id]--
}

func (w *fakeRequestWaiter) count(id string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pending[id]
}

func newRequestsFixture(t *testing.T, settle time.Duration) *mcpFixture {
	t.Helper()
	return newMCPFixtureBase(t, settle, true)
}

// autoApprove answers every pending request with an approval, for the tests
// that are about something else.
func (f *mcpFixture) autoApprove() {
	stop := make(chan struct{})
	f.t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			list, _ := f.requests.List(context.Background(), domain.ServiceRequestFilter{State: domain.ServiceRequestPending})
			for _, r := range list {
				_, _ = f.requests.Answer(context.Background(), r.ID, "approve", "")
			}
		}
	}()
}

// pendingRequests are the requests the person would see waiting.
func (f *mcpFixture) pendingRequests() []map[string]any {
	f.t.Helper()
	code, out := f.call("GET", "/api/service_requests?project_id="+f.projectID+"&state=pending", nil)
	if code != 200 {
		f.t.Fatalf("list = %d %v", code, out)
	}
	var list []map[string]any
	for _, r := range out["requests"].([]any) {
		list = append(list, r.(map[string]any))
	}
	return list
}

func (f *mcpFixture) awaitPending(n int) []map[string]any {
	f.t.Helper()
	var list []map[string]any
	eventually(f.t, "pending requests", func() bool { list = f.pendingRequests(); return len(list) == n })
	return list
}

func (f *mcpFixture) answer(id, decision, note string) (int, map[string]any) {
	f.t.Helper()
	return f.call("POST", "/api/service_requests/"+id+"/answer", map[string]any{"decision": decision, "note": note})
}

// asyncCall calls a tool in the background and gives its answer on a channel.
type toolAnswer struct {
	out map[string]any
	msg string
}

func (a mcpAgent) callAsync(tool string, args map[string]any) <-chan toolAnswer {
	ch := make(chan toolAnswer, 1)
	go func() {
		out, msg := a.call(tool, args)
		ch <- toolAnswer{out, msg}
	}()
	return ch
}

func (f *mcpFixture) waitingFor(ch <-chan toolAnswer) toolAnswer {
	f.t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(15 * time.Second):
		f.t.Fatal("the tool call did not return")
		return toolAnswer{}
	}
}

func (f *mcpFixture) stillWaiting(ch <-chan toolAnswer) {
	f.t.Helper()
	select {
	case a := <-ch:
		f.t.Fatalf("the call returned before the person answered: %v %q", a.out, a.msg)
	case <-time.After(150 * time.Millisecond):
	}
}

func requestsFixtureWithService(t *testing.T) (*mcpFixture, string) {
	t.Helper()
	f := newRequestsFixture(t, 5*time.Second)
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	in := commandInput("web")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": `echo "got D=$D"; sleep 30`}
	return f, f.createID(in)
}

func TestServicesRequests_AStartFilesARequestBlocksAndRunsOnApproval(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")

	done := agent.callAsync("start_service", map[string]any{"name": "web", "reason": "to try the build", "env": map[string]any{"D": "from-agent"}})
	reqs := f.awaitPending(1)
	f.stillWaiting(done)
	r := reqs[0]
	if r["action"] != "start" || r["service_id"] != id || r["service_name"] != "web" || r["session_id"] != "sess-1" || r["task_id"] != "task-1" ||
		r["agent"] != "dev" || r["reason"] != "to try the build" || r["state"] != "pending" || r["worktree"] != "main" {
		t.Fatalf("request = %v", r)
	}
	if s, _ := r["summary"].(string); !strings.Contains(s, "start service web") || !strings.Contains(s, "D") {
		t.Fatalf("summary = %q", s)
	}
	env := r["env"].([]any)
	if len(env) != 1 || env[0].(map[string]any)["name"] != "D" || env[0].(map[string]any)["value"] != "from-agent" || env[0].(map[string]any)["change"] != "set" {
		t.Fatalf("the person reads the override with its value: %v", env)
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("nothing starts before the answer: %s", st)
	}
	if n := f.waiter.count("sess-1"); n != 1 {
		t.Fatalf("pending approvals = %d, want 1", n)
	}

	code, out := f.answer(r["id"].(string), "approve", "")
	if code != 200 || out["request"].(map[string]any)["state"] != "done" {
		t.Fatalf("approve = %d %v", code, out)
	}
	got := f.waitingFor(done)
	if got.msg != "" {
		t.Fatalf("start refused: %s", got.msg)
	}
	run := viewRunOf(got.out)
	if got.out["already_running"] != false || run["started_by"] != "session sess-1 (dev)" || run["yours"] != true {
		t.Fatalf("answer = %v", got.out)
	}
	f.awaitLog(id, "got D=from-agent")
	if n := f.waiter.count("sess-1"); n != 0 {
		t.Fatalf("pending approvals after the answer = %d", n)
	}
	t.Cleanup(func() { f.operate("stop_service", id) })

	if code, out := f.answer(r["id"].(string), "approve", ""); code != 409 || errCode(out) != "REQUEST_NOT_PENDING" {
		t.Fatalf("answering twice = %d %v", code, out)
	}
}

func TestServicesRequests_ADenialLeavesTheServiceStoppedAndTheAgentGetsTheNote(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")

	for _, c := range []struct{ note, want string }{
		{"not now", "the person denied this service request: not now"},
		{"", "the person denied this service request"},
	} {
		done := agent.callAsync("start_service", named("web"))
		r := f.awaitPending(1)[0]
		if code, out := f.answer(r["id"].(string), "deny", c.note); code != 200 || out["request"].(map[string]any)["state"] != "denied" {
			t.Fatalf("deny = %d %v", code, out)
		}
		if got := f.waitingFor(done); got.msg != c.want {
			t.Fatalf("denied call = %v / %q, want %q", got.out, got.msg, c.want)
		}
		if st := f.status(id); st != "stopped" {
			t.Fatalf("a denied start starts nothing: %s", st)
		}
		if n := f.waiter.count("sess-1"); n != 0 {
			t.Fatalf("pending approvals = %d", n)
		}
	}
}

func TestServicesRequests_ARequestThatIsStillPendingWhenTheSessionEndsExpires(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")
	done := agent.callAsync("start_service", named("web"))
	r := f.awaitPending(1)[0]

	if err := f.cat.Bus.Publish(context.Background(), domain.SessionEnded{SessionID: "sess-1", Status: domain.SessionStopped}); err != nil {
		t.Fatal(err)
	}
	if got := f.waitingFor(done); got.msg != "the service request expired" {
		t.Fatalf("call = %v / %q", got.out, got.msg)
	}
	_, out := f.call("GET", "/api/service_requests?project_id="+f.projectID, nil)
	if got := out["requests"].([]any)[0].(map[string]any); got["id"] != r["id"] || got["state"] != "expired" {
		t.Fatalf("requests = %v", out)
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("an expired request starts nothing: %s", st)
	}
	if code, out := f.answer(r["id"].(string), "approve", ""); code != 409 || errCode(out) != "REQUEST_NOT_PENDING" {
		t.Fatalf("answering an expired request = %d %v", code, out)
	}
}

func TestServicesRequests_BadArgumentsAndNoOpActionsFileNothing(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")

	agent.refused("start_service", map[string]any{"name": "nope"}, `no service named "nope" in this project; call list_services`)
	agent.refused("start_service", map[string]any{"name": "web", "worktree": "main"}, `worktree must be "own" or omitted`)
	if _, msg := agent.call("start_service", map[string]any{"name": "web", "env": map[string]any{"1BAD": "x"}}); !strings.Contains(msg, `"1BAD"`) {
		t.Fatalf("bad env key = %q", msg)
	}
	if n := len(f.pendingRequests()); n != 0 {
		t.Fatalf("a refused call files nothing: %d pending", n)
	}

	// A service that is running is answered at once.
	f.operate("start_service", id)
	f.awaitStatus(id, "running")
	t.Cleanup(func() { f.operate("stop_service", id) })
	out := agent.ok("start_service", map[string]any{"name": "web", "env": map[string]any{"D": "x"}})
	if out["already_running"] != true {
		t.Fatalf("answer = %v", out)
	}
	if n := len(f.pendingRequests()); n != 0 {
		t.Fatalf("a no-op start files nothing: %d pending", n)
	}
}

func TestServicesRequests_ThePersonsOwnStartNeedsNoRequestAndNoValueIsInAnyAgentAnswer(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")
	const secret = "uniquely-secret-override"
	done := agent.callAsync("start_service", map[string]any{"name": "web", "env": map[string]any{"D": secret}})
	r := f.awaitPending(1)[0]
	f.answer(r["id"].(string), "approve", "")
	f.waitingFor(done)
	f.awaitLog(id, "got D="+secret)
	t.Cleanup(func() { f.operate("stop_service", id) })

	// The agent sees its own override value (ADR 0077), and no answer or log
	// holds a value of a service or the repository .env.
	if code, _ := f.operate("stop_service", id); code != 200 {
		t.Fatal("stop")
	}
	if code, out := f.operate("start_service", id); code != 200 {
		t.Fatalf("the person's start = %d %v", code, out)
	}
	f.awaitStatus(id, "running")
	b, _ := json.Marshal(f.service(id))
	if strings.Contains(string(b), secret) {
		t.Fatalf("the service holds an override value: %s", b)
	}
}

func TestServicesRequests_ARestartFilesARequestAndRestartsWithTheRepeatedOrReplacedOverrides(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")
	f.operate("start_service", id)
	first := f.awaitStatus(id, "running")["run"].(map[string]any)["id"]
	t.Cleanup(func() { f.operate("stop_service", id) })

	done := agent.callAsync("restart_service", map[string]any{"name": "web", "worktree": "own", "env": map[string]any{"D": "second"}, "reason": "new variable"})
	r := f.awaitPending(1)[0]
	f.stillWaiting(done)
	if r["action"] != "restart" || r["worktree"] != "own" || r["overrides_given"] != true || r["reason"] != "new variable" {
		t.Fatalf("request = %v", r)
	}
	if st := f.service(id)["run"].(map[string]any)["id"]; st != first {
		t.Fatalf("nothing restarts before the answer")
	}
	f.answer(r["id"].(string), "approve", "")
	got := f.waitingFor(done)
	if got.msg != "" {
		t.Fatalf("restart refused: %s", got.msg)
	}
	run := viewRunOf(got.out)
	if run["where"] != "your worktree" || run["overrides"].(map[string]any)["D"] != "second" || run["started_by"] != "session sess-1 (dev)" {
		t.Fatalf("run = %v", run)
	}
	if now := f.service(id)["run"].(map[string]any)["id"]; now == first {
		t.Fatal("a new run started")
	}
	f.awaitLog(id, "got D=second")

	// No arguments repeats the last run, and the request says so.
	done = agent.callAsync("restart_service", named("web"))
	r = f.awaitPending(1)[0]
	if r["overrides_given"] != false || !strings.Contains(r["summary"].(string), "same run overrides") {
		t.Fatalf("a bare restart = %v", r)
	}
	f.answer(r["id"].(string), "approve", "")
	if run := viewRunOf(f.waitingFor(done).out); run["overrides"].(map[string]any)["D"] != "second" || run["where"] != "your worktree" {
		t.Fatalf("repeated run = %v", run)
	}

	// env {} clears them.
	done = agent.callAsync("restart_service", map[string]any{"name": "web", "env": map[string]any{}})
	r = f.awaitPending(1)[0]
	if r["overrides_given"] != true || len(r["env"].([]any)) != 0 {
		t.Fatalf("env {} = %v", r)
	}
	f.answer(r["id"].(string), "approve", "")
	if run := viewRunOf(f.waitingFor(done).out); len(run["overrides"].(map[string]any)) != 0 {
		t.Fatalf("cleared run = %v", run)
	}
}

func TestServicesRequests_AStopFilesARequestAndStopsOnApprovalButNotOnDenial(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")

	// A service that is not running is answered at once.
	out := agent.ok("stop_service", named("web"))
	if out["service"].(map[string]any)["status"] != "stopped" || len(f.pendingRequests()) != 0 {
		t.Fatalf("a stop of a stopped service = %v", out)
	}

	f.operate("start_service", id)
	f.awaitStatus(id, "running")
	t.Cleanup(func() { f.operate("stop_service", id) })
	done := agent.callAsync("stop_service", map[string]any{"name": "web", "reason": "free the port"})
	r := f.awaitPending(1)[0]
	if r["action"] != "stop" || r["summary"] != "stop service web" || r["reason"] != "free the port" {
		t.Fatalf("request = %v", r)
	}
	f.answer(r["id"].(string), "deny", "keep it")
	if got := f.waitingFor(done); got.msg != "the person denied this service request: keep it" {
		t.Fatalf("denied stop = %v / %q", got.out, got.msg)
	}
	if st := f.status(id); st != "running" {
		t.Fatalf("a denied stop leaves it running: %s", st)
	}

	done = agent.callAsync("stop_service", named("web"))
	f.answer(f.awaitPending(1)[0]["id"].(string), "approve", "")
	got := f.waitingFor(done)
	if got.msg != "" || got.out["service"].(map[string]any)["status"] != "stopped" {
		t.Fatalf("stop = %v / %q", got.out, got.msg)
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("status = %s", st)
	}
}

func TestServicesRequests_ARestartRefusedBeforeFilingFilesNothing(t *testing.T) {
	f, _ := requestsFixtureWithService(t)
	agent := f.as("sess-1")
	agent.refused("restart_service", map[string]any{"name": "web", "worktree": "main"}, `worktree must be "own" or omitted`)
	agent.refused("restart_service", map[string]any{"name": "nope"}, `no service named "nope" in this project; call list_services`)
	if _, msg := agent.call("restart_service", map[string]any{"name": "web", "env": map[string]any{"A-B": "x"}}); !strings.Contains(msg, `"A-B"`) {
		t.Fatalf("bad env = %q", msg)
	}
	if n := len(f.pendingRequests()); n != 0 {
		t.Fatalf("%d pending", n)
	}
}

// create_service

func (f *mcpFixture) approveNext(done <-chan toolAnswer) toolAnswer {
	f.t.Helper()
	r := f.awaitPending(1)[0]
	if code, out := f.answer(r["id"].(string), "approve", ""); code != 200 {
		f.t.Fatalf("approve = %d %v", code, out)
	}
	return f.waitingFor(done)
}

func TestServicesRequests_CreateServiceFilesTheWholeDefinitionAndCreatesItOnApproval(t *testing.T) {
	f, _ := requestsFixtureWithService(t)
	agent := f.as("sess-1")

	done := agent.callAsync("create_service", map[string]any{
		"name": "api", "kind": "command", "repository": "api", "reason": "I need a dev server",
		"env":     map[string]any{"PORT": "8080", "TOKEN": "s3cret-token"},
		"command": map[string]any{"line": "npm run dev", "subdir": "", "ports": []int{8080}},
	})
	r := f.awaitPending(1)[0]
	f.stillWaiting(done)
	proposed := r["proposed"].(map[string]any)
	if r["action"] != "create" || r["service_id"] != "" || r["service_name"] != "api" || r["reason"] != "I need a dev server" ||
		proposed["name"] != "api" || proposed["kind"] != "command" || proposed["repository_id"] != f.repoID ||
		proposed["command"].(map[string]any)["line"] != "npm run dev" {
		t.Fatalf("request = %v", r)
	}
	if s, _ := r["summary"].(string); s != "create command service api: npm run dev in api" {
		t.Fatalf("summary = %q", s)
	}
	vars := r["env"].([]any)
	if len(vars) != 2 || vars[0].(map[string]any)["name"] != "PORT" || vars[1].(map[string]any)["value"] != "s3cret-token" || vars[0].(map[string]any)["change"] != "set" {
		t.Fatalf("env = %v", vars)
	}
	list := agent.ok("list_services", nil)["services"].([]any)
	for _, s := range list {
		if s.(map[string]any)["name"] == "api" {
			t.Fatal("nothing is created before the answer")
		}
	}

	got := f.approveNext(done)
	if got.msg != "" {
		t.Fatalf("create refused: %s", got.msg)
	}
	svc := got.out["service"].(map[string]any)
	if svc["name"] != "api" || svc["created_by"] != "session sess-1 (dev)" || svc["status"] != "stopped" || svc["command"] != "npm run dev" {
		t.Fatalf("service = %v", svc)
	}
	id := f.createdID("api")
	stored := f.service(id)
	if stored["env"] != "PORT=8080\nTOKEN=s3cret-token\n" && stored["env"] != "PORT=8080\nTOKEN=s3cret-token" {
		t.Fatalf("saved env = %q", stored["env"])
	}
	if by := stored["created_by"].(map[string]any); by["kind"] != "session" || by["session_id"] != "sess-1" || by["agent"] != "dev" || by["task_id"] != "task-1" {
		t.Fatalf("created_by = %v", by)
	}
	if first := f.service(f.createdID("web")); first["created_by"].(map[string]any)["kind"] != "person" {
		t.Fatalf("a service the person made = %v", first["created_by"])
	}
}

func TestServicesRequests_CreateServiceMakesAnImageServiceAndRefusesBadInputBeforeFiling(t *testing.T) {
	f, _ := requestsFixtureWithService(t)
	agent := f.as("sess-1")

	done := agent.callAsync("create_service", map[string]any{
		"name": "db", "kind": "image", "reason": "a database",
		"image": map[string]any{"ref": "postgres:16", "args": []string{"-c", "fsync=off"}, "ports": []map[string]any{{"host": 5432, "container": 5432}}},
	})
	got := f.approveNext(done)
	if got.msg != "" || got.out["service"].(map[string]any)["image"] != "postgres:16" || got.out["service"].(map[string]any)["created_by"] != "session sess-1 (dev)" {
		t.Fatalf("image create = %v / %q", got.out, got.msg)
	}

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"taken", map[string]any{"name": "web", "kind": "command", "command": map[string]any{"line": "x"}, "reason": "r"}, `a service named "web" already exists in this project`},
		{"repository", map[string]any{"name": "n1", "kind": "command", "repository": "nope", "command": map[string]any{"line": "x"}, "reason": "r"}, `no repository named "nope" in this project`},
		{"subdir", map[string]any{"name": "n2", "kind": "command", "repository": "api", "command": map[string]any{"line": "x", "subdir": "missing"}, "reason": "r"}, `command.subdir`},
		{"port", map[string]any{"name": "n3", "kind": "command", "command": map[string]any{"line": "x", "ports": []int{99999}}, "reason": "r"}, `command.ports[0]`},
		{"name", map[string]any{"name": "Bad Name", "kind": "command", "command": map[string]any{"line": "x"}, "reason": "r"}, `name must be`},
		{"key", map[string]any{"name": "n4", "kind": "command", "env": map[string]any{"1BAD": "x"}, "command": map[string]any{"line": "x"}, "reason": "r"}, `env key "1BAD" is not a valid variable name`},
		{"newline", map[string]any{"name": "n5", "kind": "image", "env": map[string]any{"X": "a\nb"}, "image": map[string]any{"ref": "x"}, "reason": "r"}, `env value of "X" contains a newline (image services)`},
		{"kind", map[string]any{"name": "n6", "kind": "vm", "reason": "r"}, `kind must be command or image`},
		{"noreason", map[string]any{"name": "n7", "kind": "command", "command": map[string]any{"line": "x"}}, `reason`},
	}
	for _, c := range cases {
		if _, msg := agent.call("create_service", c.args); !strings.Contains(msg, c.want) {
			t.Fatalf("%s: refusal = %q, want it to hold %q", c.name, msg, c.want)
		}
	}
	if n := len(f.pendingRequests()); n != 0 {
		t.Fatalf("a refused create files nothing: %d pending", n)
	}
}

func TestServicesRequests_ADeniedCreateCreatesNothing(t *testing.T) {
	f, _ := requestsFixtureWithService(t)
	agent := f.as("sess-1")
	done := agent.callAsync("create_service", map[string]any{"name": "api", "kind": "command", "command": map[string]any{"line": "x"}, "reason": "r"})
	f.answer(f.awaitPending(1)[0]["id"].(string), "deny", "no")
	if got := f.waitingFor(done); got.msg != "the person denied this service request: no" {
		t.Fatalf("denied = %q", got.msg)
	}
	list := agent.ok("list_services", nil)["services"].([]any)
	if len(list) != 1 {
		t.Fatalf("services = %v", list)
	}
}

// update_service

func updateInput(name string, changes map[string]any) map[string]any {
	return map[string]any{"name": name, "changes": changes, "reason": "tuning"}
}

func TestServicesRequests_UpdateServiceSetsReplacesAndUnsetsVariablesAndShowsOnlyItsOwnValues(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.session("sess-1", f.projectID, f.repoID, true)
	f.session("sess-2", f.projectID, f.repoID, true)
	if _, err := f.cat.Projects.SaveEnvFile(context.Background(), f.repoID, ".env", "DOTENV_ONLY=dotenv-secret\n"); err != nil {
		t.Fatal(err)
	}
	in := commandInput("web")
	in["repository_id"], in["export_repository_env"] = f.repoID, true
	in["env"] = "FROM_PERSON=person-secret\nREPLACE_ME=old\nDROP_ME=gone\n"
	in["command"] = map[string]any{"line": "make dev"}
	id := f.createID(in)
	agent, other := f.as("sess-1"), f.as("sess-2")

	done := agent.callAsync("update_service", updateInput("web", map[string]any{
		"command": map[string]any{"line": "npm run dev"},
		"env":     map[string]any{"set": map[string]any{"NEW_ONE": "mine", "REPLACE_ME": "new"}, "unset": []string{"DROP_ME"}},
	}))
	r := f.awaitPending(1)[0]
	f.stillWaiting(done)
	if r["action"] != "update" || r["service_id"] != id {
		t.Fatalf("request = %v", r)
	}
	var line map[string]any
	for _, c := range r["changes"].([]any) {
		if c.(map[string]any)["field"] == "command.line" {
			line = c.(map[string]any)
		}
		if strings.HasPrefix(c.(map[string]any)["field"].(string), "env.") && (c.(map[string]any)["before"] != "" || c.(map[string]any)["after"] != "") {
			t.Fatalf("an env change shows a value: %v", c)
		}
	}
	if line == nil || line["before"] != "make dev" || line["after"] != "npm run dev" {
		t.Fatalf("changes = %v", r["changes"])
	}
	prev := map[string]any{}
	for _, e := range r["env"].([]any) {
		m := e.(map[string]any)
		prev[m["name"].(string)+"/"+m["change"].(string)] = m["previous_set"]
	}
	if prev["NEW_ONE/set"] != false || prev["REPLACE_ME/set"] != true || prev["DROP_ME/unset"] != true {
		t.Fatalf("env changes = %v", r["env"])
	}
	if f.service(id)["command"].(map[string]any)["line"] != "make dev" {
		t.Fatal("nothing changes before the answer")
	}

	got := f.approveNext(done)
	if got.msg != "" || got.out["restart_needed"] != false {
		t.Fatalf("update = %v / %q", got.out, got.msg)
	}
	saved := f.service(id)
	for _, want := range []string{"FROM_PERSON=person-secret", "NEW_ONE=mine", "REPLACE_ME=new"} {
		if !strings.Contains(saved["env"].(string), want) {
			t.Fatalf("saved env = %q, missing %q", saved["env"], want)
		}
	}
	if strings.Contains(saved["env"].(string), "DROP_ME") || saved["command"].(map[string]any)["line"] != "npm run dev" {
		t.Fatalf("saved = %v", saved)
	}

	// The agent sees the values it set, and no one else's; another session sees none.
	view := agent.ok("service_status", named("web"))["service"].(map[string]any)
	values := view["env_values"].(map[string]any)
	if len(values) != 2 || values["NEW_ONE"] != "mine" || values["REPLACE_ME"] != "new" {
		t.Fatalf("env_values = %v", values)
	}
	if len(other.ok("service_status", named("web"))["service"].(map[string]any)["env_values"].(map[string]any)) != 0 {
		t.Fatal("another session sees values it did not set")
	}
	f.answersMu.Lock()
	all := strings.Join(f.answers, "\n")
	f.answersMu.Unlock()
	for _, secret := range []string{"person-secret", "dotenv-secret"} {
		if strings.Contains(all, secret) {
			t.Fatalf("an MCP answer holds %q", secret)
		}
	}

	// A later change by the person takes the key out of env_values.
	upd := map[string]any{"service_id": id, "service": map[string]any{
		"name": "web", "kind": "command", "repository_id": f.repoID, "export_repository_env": true,
		"env": "NEW_ONE=changed-by-person\n", "command": map[string]any{"line": "npm run dev"},
	}}
	if code, out := f.call("POST", "/api/update_service", upd); code != 200 {
		t.Fatalf("person update = %d %v", code, out)
	}
	if v := agent.ok("service_status", named("web"))["service"].(map[string]any)["env_values"].(map[string]any); len(v) != 0 {
		t.Fatalf("env_values after the person changed them = %v", v)
	}
}

func TestServicesRequests_UpdateServiceThatChangesNothingFilesNothingAndInvalidResultsAreRefused(t *testing.T) {
	f, _ := requestsFixtureWithService(t)
	agent := f.as("sess-1")

	out := agent.ok("update_service", updateInput("web", map[string]any{"name": "web", "env": map[string]any{"unset": []string{"NOT_SET"}}}))
	if out["restart_needed"] != false || len(f.pendingRequests()) != 0 {
		t.Fatalf("a no-op update = %v", out)
	}
	if out := agent.ok("update_service", updateInput("web", map[string]any{})); out["service"] == nil || len(f.pendingRequests()) != 0 {
		t.Fatalf("an empty update = %v", out)
	}

	other := commandInput("other")
	f.createID(other)
	for name, c := range map[string]struct {
		changes map[string]any
		want    string
	}{
		"taken":      {map[string]any{"name": "other"}, `a service named "other" already exists in this project`},
		"repository": {map[string]any{"repository": "nope"}, `no repository named "nope" in this project`},
		"image":      {map[string]any{"image": map[string]any{"ref": "x"}}, `image`},
		"key":        {map[string]any{"env": map[string]any{"set": map[string]any{"1BAD": "x"}}}, `env key "1BAD" is not a valid variable name`},
		"subdir":     {map[string]any{"command": map[string]any{"subdir": "missing"}}, `command.subdir`},
		"line":       {map[string]any{"command": map[string]any{"line": " "}}, `command.line`},
	} {
		if _, msg := agent.call("update_service", updateInput("web", c.changes)); !strings.Contains(msg, c.want) {
			t.Fatalf("%s: refusal = %q, want %q", name, msg, c.want)
		}
	}
	if n := len(f.pendingRequests()); n != 0 {
		t.Fatalf("refused updates file nothing: %d pending", n)
	}
}

func TestServicesRequests_UpdateServiceOfARunningServiceSaysRestartNeededAndADenialChangesNothing(t *testing.T) {
	f, id := requestsFixtureWithService(t)
	agent := f.as("sess-1")
	f.operate("start_service", id)
	f.awaitStatus(id, "running")
	t.Cleanup(func() { f.operate("stop_service", id) })

	done := agent.callAsync("update_service", updateInput("web", map[string]any{"command": map[string]any{"line": "sleep 31"}}))
	r := f.awaitPending(1)[0]
	f.answer(r["id"].(string), "deny", "")
	if got := f.waitingFor(done); got.msg != "the person denied this service request" {
		t.Fatalf("denied = %q", got.msg)
	}
	if f.service(id)["command"].(map[string]any)["line"] == "sleep 31" {
		t.Fatal("a denied update changed the service")
	}

	done = agent.callAsync("update_service", updateInput("web", map[string]any{"command": map[string]any{"line": "sleep 31"}}))
	got := f.approveNext(done)
	if got.msg != "" || got.out["restart_needed"] != true {
		t.Fatalf("update of a running service = %v / %q", got.out, got.msg)
	}
	if view := got.out["service"].(map[string]any); view["pending_request"] != nil {
		t.Fatalf("pending_request = %v", view["pending_request"])
	}
}

func TestServicesRequests_ServiceViewShowsThisSessionsPendingRequest(t *testing.T) {
	f, _ := requestsFixtureWithService(t)
	f.session("sess-2", f.projectID, f.repoID, true)
	agent, other := f.as("sess-1"), f.as("sess-2")
	done := agent.callAsync("start_service", named("web"))
	r := f.awaitPending(1)[0]

	mine := agent.ok("service_status", named("web"))["service"].(map[string]any)["pending_request"].(map[string]any)
	if mine["id"] != r["id"] || mine["action"] != "start" {
		t.Fatalf("pending_request = %v", mine)
	}
	if other.ok("service_status", named("web"))["service"].(map[string]any)["pending_request"] != nil {
		t.Fatal("another session sees a request that is not its own")
	}
	f.answer(r["id"].(string), "deny", "")
	f.waitingFor(done)
}
