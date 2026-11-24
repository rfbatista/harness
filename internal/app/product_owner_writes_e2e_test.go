//go:build !windows

package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"

	"operators-mcp/internal/domain"
)

// ownerSession starts a thread and dials its session's tools, as the claude
// the server launched would.
func (e *poE2E) ownerSession() (threadID string, c *client.Client) {
	e.t.Helper()
	id, _ := e.startThread("shape a feature")
	e.waitTurn(id, "idle")
	return id, e.dialSession(e.thread(id).Thread.SessionID)
}

// madeTask calls a task tool of the product owner and decodes the task it
// answers.
func (e *poE2E) madeTask(c *client.Client, tool string, args map[string]any) attributedRow {
	e.t.Helper()
	text, refused := e.call(c, tool, args)
	if refused {
		e.t.Fatalf("%s %v was refused: %s", tool, args, text)
	}
	var out struct {
		Task attributedRow `json:"task"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.Task.ID == "" {
		e.t.Fatalf("%s answered %s", tool, text)
	}
	return out.Task
}

func TestTheProductOwnerCreatesAFeatureTaskAndItsChildAttributedToItsThread(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()

	feature := e.madeTask(owner, "create_task", map[string]any{"title": "Feature", "description": "What it is", "parent_id": ""})
	child := e.madeTask(owner, "create_task", map[string]any{"title": "Child", "parent_id": feature.ID})

	for _, id := range []string{feature.ID, child.ID} {
		row := e.taskRow(id)
		if row.CreatedBy == nil || row.CreatedBy.Kind != "product_owner" || row.CreatedBy.ThreadID != threadID || row.CreatedBy.At == "" {
			t.Errorf("task %s created_by = %+v, want the product owner with thread %s", id, row.CreatedBy, threadID)
		}
	}
	var got struct {
		Ticket struct {
			Status   string `json:"status"`
			ParentID string `json:"parent_id"`
		} `json:"ticket"`
	}
	e.get("get_ticket?ticket_id="+child.ID, &got)
	if got.Ticket.ParentID != feature.ID || got.Ticket.Status != "backlog" {
		t.Errorf("the child = %+v, want a backlog child of the feature", got.Ticket)
	}
	eventuallyPO(t, "the board's feed carries the product owner's tasks", func() string {
		n := 0
		for _, c := range e.feed.changesOf("ticket") {
			if strings.Contains(c, "product_owner") {
				n++
			}
		}
		if n < 2 {
			return "ticket events by the product owner on the feed: " + string(rune('0'+n))
		}
		return ""
	})
}

func TestTheProductOwnerChangesATasksTitleDescriptionAndStatusAndRecordsItself(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	made := e.madeTask(owner, "create_task", map[string]any{"title": "Draft", "parent_id": ""})

	e.madeTask(owner, "update_task", map[string]any{"task_id": made.ID, "title": "Final", "description": "Now described", "status": "review"})

	var got struct {
		Ticket struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Status      string `json:"status"`
		} `json:"ticket"`
	}
	e.get("get_ticket?ticket_id="+made.ID, &got)
	if got.Ticket.Title != "Final" || got.Ticket.Description != "Now described" || got.Ticket.Status != "review" {
		t.Errorf("the task = %+v, want the changed title, description and status", got.Ticket)
	}
	if row := e.taskRow(made.ID); row.ChangedBy == nil || row.ChangedBy.Kind != "product_owner" || row.ChangedBy.ThreadID != threadID {
		t.Errorf("changed_by = %+v, want the product owner with thread %s", row.ChangedBy, threadID)
	}
}

func TestTheProductOwnerCannotMoveATaskToDoneNorChangeATaskOfAnotherProject(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerSession()
	made := e.madeTask(owner, "create_task", map[string]any{"title": "Work", "parent_id": ""})

	text, refused := e.call(owner, "update_task", map[string]any{"task_id": made.ID, "status": "done", "title": "Should not apply"})
	if !refused || !strings.Contains(text, "Only the person moves a task to done") {
		t.Fatalf("a move to done = %q refused=%v, want only the person moves a task to done", text, refused)
	}
	var got struct {
		Ticket struct{ Title, Status string } `json:"ticket"`
	}
	e.get("get_ticket?ticket_id="+made.ID, &got)
	if got.Ticket.Status != "backlog" || got.Ticket.Title != "Work" {
		t.Errorf("after a refused move the task = %+v, want it unchanged", got.Ticket)
	}
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.plan.CreateTicket(e.ctx, other.ID, "Not yours", "", domain.TicketStatusBacklog, "")
	if err != nil {
		t.Fatal(err)
	}
	if text, refused := e.call(owner, "update_task", map[string]any{"task_id": foreign.ID, "title": "Changed"}); !refused || !strings.Contains(text, "TASK_NOT_FOUND") {
		t.Errorf("a task of another project = %q refused=%v, want TASK_NOT_FOUND", text, refused)
	}
	if again, _ := e.plan.GetTicket(e.ctx, foreign.ID); again.Title != "Not yours" {
		t.Error("the product owner changed a task of another project")
	}
}

func TestTheProductOwnerIsServedTaskWriteToolsButNoDeleteNorStartAndNoOtherModeGetsItsTools(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerSession()

	got := e.toolNames(owner)
	for _, want := range []string{"create_task", "update_task"} {
		if !slices.Contains(got, want) {
			t.Errorf("the product owner is not served %s: %v", want, got)
		}
	}
	for _, name := range got {
		if strings.Contains(name, "delete") || strings.Contains(name, "start") || strings.Contains(name, "archive") {
			t.Errorf("the product owner is served %s", name)
		}
	}
	for _, banned := range []string{"start_task_session", "delete_ticket", "delete_document", "update_task_status", "archive_ticket"} {
		if text, refused := e.call(owner, banned, map[string]any{"task_id": "x", "prompt": "x", "status": "done"}); !refused {
			t.Errorf("a call to %s was answered: %s", banned, text)
		}
	}

	// Another mode's session keeps its own tools and none of the product owner's.
	if _, err := e.sess.Create(&domain.Session{ID: "product-session", ProjectID: e.project, Task: "a product session", Status: domain.SessionRunning, Mode: domain.SessionModeProduct}); err != nil {
		t.Fatal(err)
	}
	other := e.toolNames(e.dialSession("product-session"))
	if slices.Contains(other, "update_task") || slices.Contains(other, "get_project_task") {
		t.Errorf("a product session is served the product owner's tools: %v", other)
	}
	if !slices.Contains(other, "create_task") || !slices.Contains(other, "update_task_status") {
		t.Errorf("a product session lost its own tools: %v", other)
	}
}

func TestTheOpeningBriefNamesTheProductOwnersTaskWritesAndTheLinesItNeverCrosses(t *testing.T) {
	e := newPOE2E(t)
	e.ownerSession()
	l := e.launches()
	if len(l) != 1 {
		t.Fatalf("launches = %q", l)
	}
	for _, want := range []string{"update_task", "only the person moves a task to done", "never delete", "start a session"} {
		if !strings.Contains(strings.ToLower(l[0]), strings.ToLower(want)) {
			t.Errorf("the opening brief does not say %q", want)
		}
	}
}
