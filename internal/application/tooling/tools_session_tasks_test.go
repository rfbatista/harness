package tooling

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"
)

// A product session on a feature task creates its children: with no parent_id
// given, the new task hangs off the session's task.
func TestSessionTaskTools_CreateTask_DefaultsParentToSessionTask(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "create_task", map[string]any{"title": "Export endpoint", "description": "GET /export"})
	if err != nil {
		t.Fatal(err)
	}
	tk := out.(map[string]any)["task"].(*domain.Ticket)
	if tk.ParentID != f.ticketID || tk.ProjectID != f.projectID {
		t.Fatalf("child not under the session's task in its project: %+v", tk)
	}
	if tk.Title != "Export endpoint" || tk.Description != "GET /export" || tk.Status != domain.TicketStatusBacklog {
		t.Fatalf("wrong task: %+v", tk)
	}
}

// An explicit empty parent_id is how a session creates a top-level task.
func TestSessionTaskTools_CreateTask_EmptyParentIsTopLevel(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "create_task", map[string]any{"title": "Next feature", "parent_id": ""})
	if err != nil {
		t.Fatal(err)
	}
	tk := out.(map[string]any)["task"].(*domain.Ticket)
	if tk.ParentID != "" || tk.ProjectID != f.projectID {
		t.Fatalf("want a top-level task of the project, got %+v", tk)
	}
}

// The parent rule belongs to planning: a parent outside the session's project
// is refused, and the tool passes the code through.
func TestSessionTaskTools_CreateTask_ParentFromAnotherProjectIsInvalid(t *testing.T) {
	f := newTaskToolsFixture(t)
	other, err := f.projects.Create("other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := f.plan.CreateTicket(context.Background(), other.ID, "Theirs", "", domain.TicketStatusBacklog, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.call(t, f.sessionID, "create_task", map[string]any{"title": "Mine", "parent_id": foreign.ID})
	wantCode(t, err, domain.CodeTicketParentInvalid)
}

// A session started on a project without a task (a product session shaping a
// feature) creates the feature task itself: with no parent to default to and
// none given, the task is top-level.
func TestSessionTaskTools_CreateTask_SessionWithoutTaskCreatesTopLevel(t *testing.T) {
	f := newTaskToolsFixture(t)
	if _, err := f.sessions.Create(&domain.Session{ID: "free", ProjectID: f.projectID, Task: "shape it", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	out, err := f.call(t, "free", "create_task", map[string]any{"title": "The feature"})
	if err != nil {
		t.Fatal(err)
	}
	tk := out.(map[string]any)["task"].(*domain.Ticket)
	if tk.ParentID != "" || tk.ProjectID != f.projectID || tk.Title != "The feature" {
		t.Fatalf("want a top-level task of the session's project, got %+v", tk)
	}

	_, err = f.call(t, "free", "create_task", map[string]any{"title": ""})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, "", "create_task", map[string]any{"title": "x"})
	wantCode(t, err, "SESSION_NOT_FOUND")
}

// list_project_tasks shows the project's tasks as id, title, status and
// parent_id; with parent_id, only the children of that task.
func TestSessionTaskTools_ListProjectTasks(t *testing.T) {
	f := newTaskToolsFixture(t)
	child, err := f.plan.CreateTicket(context.Background(), f.projectID, "Child", "", domain.TicketStatusBacklog, f.ticketID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.projects.Create("other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.plan.CreateTicket(context.Background(), other.ID, "Theirs", "", domain.TicketStatusBacklog, ""); err != nil {
		t.Fatal(err)
	}

	list := func(args map[string]any) []map[string]any {
		t.Helper()
		out, err := f.call(t, f.sessionID, "list_project_tasks", args)
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]any)
		tasks := m["tasks"].([]map[string]any)
		if m["count"] != len(tasks) {
			t.Fatalf("count %v for %d tasks", m["count"], len(tasks))
		}
		return tasks
	}

	all := list(nil)
	if len(all) != 2 {
		t.Fatalf("want the project's 2 tasks, got %+v", all)
	}
	byID := map[string]map[string]any{}
	for _, tk := range all {
		byID[tk["id"].(string)] = tk
	}
	if got := byID[child.ID]; got["title"] != "Child" || got["status"] != domain.TicketStatusBacklog || got["parent_id"] != f.ticketID {
		t.Fatalf("child listed as %+v", got)
	}
	if got := byID[f.ticketID]; got["parent_id"] != "" {
		t.Fatalf("feature task listed as %+v", got)
	}

	children := list(map[string]any{"parent_id": f.ticketID})
	if len(children) != 1 || children[0]["id"] != child.ID {
		t.Fatalf("want only the child, got %+v", children)
	}
	top := list(map[string]any{"parent_id": ""})
	if len(top) != 1 || top[0]["id"] != f.ticketID {
		t.Fatalf("want only the top-level task, got %+v", top)
	}
}
