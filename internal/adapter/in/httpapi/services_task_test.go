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
	eventually(t, "task-1 has nothing pending", func() bool { return len(list("&state=pending&task_id=task-1")) == 0 })
	if got := list("&state=pending&task_id=task-2"); !slices.Equal(got, []string{"two"}) {
		t.Errorf("task-2 pending = %v", got)
	}
	f.answer(byTask["task-2"], "deny", "")
	f.waitingFor(two)
}
