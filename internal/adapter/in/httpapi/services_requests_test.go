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
