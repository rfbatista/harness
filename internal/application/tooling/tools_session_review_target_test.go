package tooling

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"
)

// freeSession records a session on the fixture's project with no task of its
// own: a project-level product session (ADR 0016).
func (f *channelFixture) freeSession(t *testing.T, id string) {
	t.Helper()
	if _, err := f.sessions.Create(&domain.Session{ID: id, ProjectID: f.projectID, Mode: domain.SessionModeProduct, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
}

// A session with no task names the feature task it created: the review
// opens on that task, and the status moves on it.
func TestReviewTarget_TicketlessSessionNamesTheTask(t *testing.T) {
	f := newChannelFixture(t)
	f.freeSession(t, "pm")

	out := f.mustCall(t, "pm", "request_user_review", map[string]any{
		"task_id": f.taskID, "subject": "PRD: export", "body": "the PRD; children tk-a and tk-b", "document_ids": []any{"d1"},
	})
	if !f.ch.called("RequestUserReviewOn") || f.ch.called("RequestUserReview") {
		t.Fatalf("calls = %v, want RequestUserReviewOn only", f.ch.calls)
	}
	if f.ch.caller != "pm" || f.ch.target != f.taskID || f.ch.review.Subject != "PRD: export" || len(f.ch.review.DocumentIDs) != 1 {
		t.Fatalf("review %+v on %q by %q", f.ch.review, f.ch.target, f.ch.caller)
	}
	if rr := out["review_request"].(map[string]any); rr["task_id"] != f.taskID || rr["state"] != "pending" {
		t.Fatalf("out = %+v", out)
	}

	out = f.mustCall(t, "pm", "update_task_status", map[string]any{"task_id": f.taskID, "status": "review", "reason": "ready for review"})
	if !f.ch.called("SetTaskStatusOn") || f.ch.called("SetTaskStatus") {
		t.Fatalf("calls = %v, want SetTaskStatusOn only", f.ch.calls)
	}
	if f.ch.caller != "pm" || f.ch.target != f.taskID || f.ch.status != domain.TicketStatusReview || f.ch.reason != "ready for review" {
		t.Fatalf("moved %q to %q by %q", f.ch.target, f.ch.status, f.ch.caller)
	}
	if out["task"].(map[string]any)["status"] != "review" {
		t.Fatalf("out = %+v", out)
	}
}

// A session on a task acts on that task: a task_id is refused, and the
// port is not called.
func TestReviewTarget_SessionWithATaskRefusesTaskID(t *testing.T) {
	f := newChannelFixture(t)
	_, err := f.call(t, "arch", "request_user_review", map[string]any{"task_id": f.taskID, "subject": "s", "body": "b"})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, "arch", "update_task_status", map[string]any{"task_id": f.taskID, "status": "review"})
	wantCode(t, err, "INVALID_INPUT")
	if len(f.ch.calls) != 0 {
		t.Fatalf("port called: %v", f.ch.calls)
	}
	// Without task_id the on-task path is unchanged.
	f.mustCall(t, "arch", "update_task_status", map[string]any{"status": "review"})
	if !f.ch.called("SetTaskStatus") {
		t.Fatalf("calls = %v, want SetTaskStatus", f.ch.calls)
	}
}

// A task of another project is out of reach, before the port is called.
func TestReviewTarget_RefusesATaskOfAnotherProject(t *testing.T) {
	f := newChannelFixture(t)
	f.freeSession(t, "pm")
	other, err := f.projects.Create("other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := f.plan.CreateTicket(context.Background(), other.ID, "Theirs", "", domain.TicketStatusTodo, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.call(t, "pm", "request_user_review", map[string]any{"task_id": theirs.ID, "subject": "s", "body": "b"})
	wantCode(t, err, "TASK_NOT_ON_PROJECT")
	_, err = f.call(t, "pm", "update_task_status", map[string]any{"task_id": theirs.ID, "status": "review"})
	wantCode(t, err, "TASK_NOT_ON_PROJECT")
	_, err = f.call(t, "pm", "update_task_status", map[string]any{"task_id": "missing", "status": "review"})
	wantCode(t, err, "TICKET_NOT_FOUND")
	if len(f.ch.calls) != 0 {
		t.Fatalf("port called: %v", f.ch.calls)
	}
}

// A session with no task and no task_id has nothing to act on.
func TestReviewTarget_TicketlessSessionWithoutTaskID(t *testing.T) {
	f := newChannelFixture(t)
	f.freeSession(t, "pm")
	_, err := f.call(t, "pm", "request_user_review", map[string]any{"subject": "s", "body": "b"})
	wantCode(t, err, "SESSION_HAS_NO_TASK")
	_, err = f.call(t, "pm", "update_task_status", map[string]any{"status": "review"})
	wantCode(t, err, "SESSION_HAS_NO_TASK")
	if len(f.ch.calls) != 0 {
		t.Fatalf("port called: %v", f.ch.calls)
	}
}

// Without a channel, update_task_status with task_id still moves the task
// through planning, as the session.
func TestReviewTarget_UpdateStatusWithoutAChannel(t *testing.T) {
	f := newTaskToolsFixture(t)
	if _, err := f.sessions.Create(&domain.Session{ID: "pm", ProjectID: f.projectID, Mode: domain.SessionModeProduct, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	out, err := f.call(t, "pm", "update_task_status", map[string]any{"task_id": f.ticketID, "status": "review", "reason": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if tk := out.(map[string]any)["task"].(*domain.Ticket); tk.ID != f.ticketID || tk.Status != domain.TicketStatusReview {
		t.Fatalf("task = %+v", tk)
	}
}
