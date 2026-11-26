package httpapi

import (
	"slices"
	"sort"
	"testing"
	"time"
)

// A task's services (serviços da task) and its pending service requests,
// asked of the services HTTP API with task_id.

func serviceNames(out map[string]any, key string) []string {
	var names []string
	for _, s := range out[key].([]any) {
		m := s.(map[string]any)
		if n, ok := m["name"].(string); ok {
			names = append(names, n)
		} else {
			names = append(names, m["service_name"].(string))
		}
	}
	sort.Strings(names)
	return names
}

func (f *mcpFixture) taskServices(taskID string) []string {
	f.t.Helper()
	path := "/api/list_services?project_id=" + f.projectID
	if taskID != "" {
		path += "&task_id=" + taskID
	}
	code, out := f.call("GET", path, nil)
	if code != 200 {
		f.t.Fatalf("list_services = %d %v", code, out)
	}
	return serviceNames(out, "services")
}

func TestTaskServices_ListHoldsOnlyTheServicesTheTaskCreatedOrRan(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.autoApprove()
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	f.ticketID = "task-2"
	f.session("sess-2", f.projectID, f.repoID, true)

	// task-1's session creates alpha; task-2's session starts a service the
	// person made; gamma is touched by no task.
	f.as("sess-1").ok("create_service", map[string]any{
		"name": "alpha", "kind": "command", "repository": "api", "reason": "r",
		"command": map[string]any{"line": "sleep 30"},
	})
	beta := f.commandIn(f.repoID, "beta", "sleep 30", "")
	f.commandIn(f.repoID, "gamma", "sleep 30", "")
	f.as("sess-2").ok("start_service", named("beta"))
	t.Cleanup(func() { f.operate("stop_service", beta) })

	for _, c := range []struct {
		task string
		want []string
	}{
		{"task-1", []string{"alpha"}},
		{"task-2", []string{"beta"}},
		{"task-3", nil},
		{"", []string{"alpha", "beta", "gamma"}},
	} {
		if got := f.taskServices(c.task); !slices.Equal(got, c.want) {
			t.Errorf("services of task %q = %v, want %v", c.task, got, c.want)
		}
	}
}

func TestTaskServices_ARequestListHoldsOnlyTheTasksPendingRequests(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	f.ticketID = "task-2"
	f.session("sess-2", f.projectID, f.repoID, true)
	f.commandIn(f.repoID, "one", "sleep 30", "")
	f.commandIn(f.repoID, "two", "sleep 30", "")

	one := f.as("sess-1").callAsync("start_service", named("one"))
	two := f.as("sess-2").callAsync("start_service", named("two"))
	f.awaitPending(2)

	list := func(query string) []string {
		code, out := f.call("GET", "/api/service_requests?project_id="+f.projectID+query, nil)
		if code != 200 {
			t.Fatalf("service_requests = %d %v", code, out)
		}
		return serviceNames(out, "requests")
	}
	if got := list("&state=pending&task_id=task-1"); !slices.Equal(got, []string{"one"}) {
		t.Errorf("task-1 pending = %v", got)
	}
	if got := list("&task_id=task-1"); !slices.Equal(got, []string{"one"}) {
		t.Errorf("task-1 with no state = %v, want its pending request", got)
	}
	if got := list("&state=pending&task_id=task-3"); len(got) != 0 {
		t.Errorf("task-3 pending = %v", got)
	}
	if got := list("&state=pending"); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("all pending = %v", got)
	}

	// An answered request leaves the task's pending list.
	r := f.awaitPending(2)
	byTask := map[string]string{}
	for _, req := range r {
		byTask[req["task_id"].(string)] = req["id"].(string)
	}
	f.answer(byTask["task-1"], "deny", "")
	f.waitingFor(one)
	eventually(t, "task-1 has nothing pending", func() bool { return len(list("&task_id=task-1")) == 0 })
	if got := list("&state=pending&task_id=task-2"); !slices.Equal(got, []string{"two"}) {
		t.Errorf("task-2 pending = %v", got)
	}
	f.answer(byTask["task-2"], "deny", "")
	f.waitingFor(two)
}

func (f *mcpFixture) personAction(verb, id, taskID string) {
	f.t.Helper()
	body := map[string]any{"service_id": id}
	if taskID != "" {
		body["task_id"] = taskID
	}
	if code, out := f.call("POST", "/api/"+verb, body); code != 200 {
		f.t.Fatalf("%s = %d %v", verb, code, out)
	}
}

func startedBy(svc map[string]any) map[string]any {
	return svc["run"].(map[string]any)["started_by"].(map[string]any)
}

func TestTaskServices_APersonRestartFromTheTaskPageRecordsTheTaskOnTheRun(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	id := f.commandIn(f.repoID, "web", "sleep 30", "")
	f.personAction("start_service", id, "")
	t.Cleanup(func() { f.operate("stop_service", id) })
	if got := f.taskServices("task-9"); len(got) != 0 {
		t.Fatalf("a person's own start belongs to no task: %v", got)
	}

	f.personAction("restart_service", id, "task-9")
	f.awaitStatus(id, "running")
	if by := startedBy(f.service(id)); by["kind"] != "person" || by["task_id"] != "task-9" {
		t.Fatalf("started_by = %v", by)
	}
	if got := f.taskServices("task-9"); !slices.Equal(got, []string{"web"}) {
		t.Fatalf("services of task-9 = %v", got)
	}

	f.personAction("restart_service", id, "")
	f.awaitStatus(id, "running")
	if by := startedBy(f.service(id)); by["task_id"] != "" {
		t.Fatalf("a restart with no task records none: %v", by)
	}
}

func TestTaskServices_AStopFromTheTaskPageKeepsTheServiceInTheTasksList(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.autoApprove()
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	id := f.commandIn(f.repoID, "web", "sleep 30", "")
	f.as("sess-1").ok("start_service", named("web"))
	f.awaitStatus(id, "running")

	f.personAction("stop_service", id, "task-1")
	f.awaitStatus(id, "stopped")
	if by := startedBy(f.service(id)); by["kind"] != "session" || by["task_id"] != "task-1" {
		t.Fatalf("a stop keeps who started the run: %v", by)
	}
	if got := f.taskServices("task-1"); !slices.Equal(got, []string{"web"}) {
		t.Fatalf("services of task-1 after the stop = %v", got)
	}
}

func TestTaskServices_APersonStartFromTheTaskPageRecordsTheTaskOnTheRun(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	id := f.commandIn(f.repoID, "web", "sleep 30", "")
	t.Cleanup(func() { f.operate("stop_service", id) })

	f.personAction("start_service", id, "task-9")
	f.awaitStatus(id, "running")
	if by := startedBy(f.service(id)); by["kind"] != "person" || by["task_id"] != "task-9" {
		t.Fatalf("started_by = %v", by)
	}
	if got := f.taskServices("task-9"); !slices.Equal(got, []string{"web"}) {
		t.Fatalf("services of task-9 = %v", got)
	}
}
