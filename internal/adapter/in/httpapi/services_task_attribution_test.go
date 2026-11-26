package httpapi

import (
	"slices"
	"testing"
	"time"
)

// Gaps the task-services seam left: a task's list after a delete, a restart
// by the person on a service another task made, and the attribution the panel
// filters feed events by.

func TestTaskServices_ADeletedServiceLeavesTheTasksList(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.autoApprove()
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	f.as("sess-1").ok("create_service", map[string]any{
		"name": "alpha", "kind": "command", "repository": "api", "reason": "r",
		"command": map[string]any{"line": "sleep 30"},
	})
	if got := f.taskServices("task-1"); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("services of task-1 = %v", got)
	}
	if code, out := f.call("POST", "/api/delete_service", map[string]any{"service_id": f.createdID("alpha")}); code != 200 {
		t.Fatalf("delete = %d %v", code, out)
	}
	if got := f.taskServices("task-1"); len(got) != 0 {
		t.Fatalf("a deleted service is still listed for the task: %v", got)
	}
}

func TestTaskServices_ARestartFromAnotherTasksPageKeepsTheCreatorsListToo(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.autoApprove()
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	f.as("sess-1").ok("create_service", map[string]any{
		"name": "alpha", "kind": "command", "repository": "api", "reason": "r",
		"command": map[string]any{"line": "sleep 30"},
	})
	id := f.createdID("alpha")
	f.personAction("restart_service", id, "task-2")
	t.Cleanup(func() { f.operate("stop_service", id) })
	f.awaitStatus(id, "running")

	for _, task := range []string{"task-1", "task-2"} {
		if got := f.taskServices(task); !slices.Equal(got, []string{"alpha"}) {
			t.Errorf("services of %s = %v, want [alpha]", task, got)
		}
	}
	if got := f.taskServices("task-3"); len(got) != 0 {
		t.Errorf("services of task-3 = %v", got)
	}
}

func TestTaskServices_TheFeedEventOfATaskRestartCarriesTheTaskForThePanelToFilterBy(t *testing.T) {
	f := newMCPFixture(t, 5*time.Second)
	id := f.commandIn(f.repoID, "web", "sleep 30", "")
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)
	t.Cleanup(func() { f.operate("stop_service", id) })

	f.personAction("restart_service", id, "task-9")
	for {
		e := nextEvent(t, events)
		if e["kind"] != "service_changed" {
			continue
		}
		svc := e["service"].(map[string]any)
		run, _ := svc["run"].(map[string]any)
		if by, _ := run["started_by"].(map[string]any); by["task_id"] == "task-9" {
			if by["kind"] != "person" {
				t.Fatalf("started_by = %v", by)
			}
			return
		}
	}
}
