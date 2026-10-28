package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeBringUnder stands in for the architect channel's move.
type fakeBringUnder struct {
	plan    *ports.BringUnderPlan
	planErr error

	planned  []string // task ids planned
	movedTo  string   // architect the move named
	movedIDs []string
}

func (f *fakeBringUnder) BringSessionsUnder(context.Context, string, []string) (*ports.BringUnderResult, error) {
	return nil, nil
}

func (f *fakeBringUnder) PlanBringUnder(_ context.Context, taskID string, _ []string) (*ports.BringUnderPlan, error) {
	f.planned = append(f.planned, taskID)
	return f.plan, f.planErr
}

func (f *fakeBringUnder) BringUnderNewArchitect(_ context.Context, architectID string, ids []string) (*ports.BringUnderResult, error) {
	f.movedTo, f.movedIDs = architectID, ids
	return &ports.BringUnderResult{}, nil
}

func spunSession(id string, status domain.SessionStatus, task string) *domain.Session {
	return &domain.Session{ID: id, Status: status, Task: task, Branch: "feat/" + id, LastAction: "Read x", CreatedAt: time.Now().Add(-7 * time.Minute)}
}

func TestStartInteractive_ArchitectOverASelectionIsOpenedToSupervise(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.tickets.(fakeTickets)["tk1"].Description = "Users export their data as CSV."
	bu := &fakeBringUnder{plan: &ports.BringUnderPlan{Eligible: []*domain.Session{
		spunSession("s-a", domain.SessionRunning, "build the thing"),
		spunSession("s-b", domain.SessionDone, "spike\nsecond line"),
	}}}
	svc.BringUnder = bu

	sess, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect", BringUnderSessionIDs: []string{"s-a", "s-b"}, Prompt: "Mind the deadline."})

	want := `Use the task-architecture skill on task tk1: Ship the thing

Users export their data as CSV.

You were started to supervise sessions the person brought under you; the work is already underway, so do not grill the person on a design and do not write specs or start new delegates unless they ask. Your delegates:

- claude session s-a · status running · branch feat/s-a · brief "build the thing" · last action "Read x" · started 7m ago
- claude session s-b · status done · branch feat/s-b · brief "spike second line" · last action "Read x" · started 7m ago

Start by reading list_task_sessions and list_task_messages. Supervise them: answer what they report with reply_to_session, tune or pause each loop with set_status_check (the running ones have one at the default interval), ask the person for review with request_user_review when their work is ready, and move the task status with update_task_status and a reason as the work moves; the person moves it to done. When the person adds a resource or a comment to the task, a turn starting with [task update · …] names its file; read it. Leave the person a comment for decisions and summaries they should see by writing a file in task/comments/; talk to your delegates with task messages, not comments.

Mind the deadline.`
	if launch.Spec.Prompt != want {
		t.Fatalf("opening:\n%s\nwant:\n%s", launch.Spec.Prompt, want)
	}
	if bu.movedTo != sess.ID || len(bu.movedIDs) != 2 || bu.movedIDs[0] != "s-a" {
		t.Fatalf("move: to %q ids %v, want the new architect %q", bu.movedTo, bu.movedIDs, sess.ID)
	}
	if sess.Mode != domain.SessionModeArchitect || sess.ParentSessionID != "" {
		t.Fatalf("the architect is a plain architect-mode session: %+v", sess)
	}
}

func TestStartInteractive_WithoutASelectionTheArchitectOpeningIsUnchanged(t *testing.T) {
	svc, _ := newInteractiveService(t)
	bu := &fakeBringUnder{}
	svc.BringUnder = bu
	for _, ids := range [][]string{nil, {}} {
		_, launch := startInteractive(t, svc, InteractiveRequest{Mode: "architect", BringUnderSessionIDs: ids})
		if !strings.Contains(launch.Spec.Prompt, "First grill the person on the design") {
			t.Fatalf("the usual opening: %s", launch.Spec.Prompt)
		}
	}
	if len(bu.planned) != 0 || bu.movedTo != "" {
		t.Fatal("no selection, no bring-under")
	}
}

func TestStartInteractive_ASelectionIsCheckedBeforeAnythingIsCreated(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		planErr error
		want    string
	}{
		{"not architect mode", "implement", nil, "INVALID_INPUT"},
		{"a running architect exists", "architect", &domain.StructuredError{Code: "ARCHITECT_EXISTS", Message: "ARCHITECT_EXISTS: x"}, "ARCHITECT_EXISTS"},
		{"nothing eligible", "architect", &domain.StructuredError{Code: "NOTHING_TO_MOVE", Message: "NOTHING_TO_MOVE: x"}, "NOTHING_TO_MOVE"},
	}
	for _, c := range cases {
		svc, prov := newInteractiveService(t)
		svc.UseBranchNames(&stubBranchNames{check: &domain.BranchNameCheck{Valid: true, Available: true}})
		bu := &fakeBringUnder{planErr: c.planErr}
		svc.BringUnder = bu
		_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{
			ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: c.mode, BranchName: "feat", BringUnderSessionIDs: []string{"s-a"},
		})
		if codeOf(err) != c.want {
			t.Errorf("%s: err %v, want %s", c.name, err, c.want)
		}
		if len(prov.created) != 0 {
			t.Errorf("%s: a worktree was made", c.name)
		}
		if bu.movedTo != "" {
			t.Errorf("%s: sessions were moved", c.name)
		}
	}
}
