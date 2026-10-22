package taskchannel_test

import (
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// startFree records a session on the project with no task of its own: a
// project-level product session (ADR 0016).
func (f *fixture) startFree(t *testing.T, id string) *domain.Session {
	t.Helper()
	s, err := f.sessions.Create(&domain.Session{ID: id, ProjectID: f.project, Mode: domain.SessionModeProduct, Status: domain.SessionRunning, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A session with no task opens the review on the feature task it names, as
// if it were its architect: the review is pending on that task, the task
// moves under the same rule, and the person's answer comes back to it.
func TestProjectSession_ReviewsAndMovesATaskOfItsProject(t *testing.T) {
	f := newFixture(t)
	f.startFree(t, "pm")

	r, err := f.svc.RequestUserReviewOn(ctx, "pm", f.ticket, ports.ReviewRequestInput{Subject: "PRD: export", Body: "the PRD and two children"})
	if err != nil {
		t.Fatal(err)
	}
	if r.TaskID != f.ticket || r.ProjectID != f.project || r.ArchitectSessionID != "pm" || r.State != domain.ReviewPending {
		t.Fatalf("review = %+v", r)
	}
	pending, _ := f.svc.ListReviewRequests(ctx, f.ticket, domain.ReviewPending)
	if len(pending) != 1 || pending[0].ID != r.ID {
		t.Fatalf("pending on the task: %+v", pending)
	}
	if tk, _ := f.plan.GetTicket(ctx, f.ticket); tk.PendingReviews != 1 {
		t.Fatalf("the task counts %d pending reviews, want 1", tk.PendingReviews)
	}

	tk, err := f.svc.SetTaskStatusOn(ctx, "pm", f.ticket, domain.TicketStatusReview, "PRD and briefs written")
	if err != nil || tk.Status != domain.TicketStatusReview {
		t.Fatalf("move to review: %+v %v", tk, err)
	}

	if _, delivered, err := f.svc.RespondReview(ctx, r.ID, domain.ReviewApproved, ""); err != nil || !delivered {
		t.Fatalf("respond: delivered %v, %v", delivered, err)
	}
	if got := f.del.got("pm"); len(got) != 1 {
		t.Fatalf("the answer must reach the session that asked: %v", got)
	}
}

// task_id is only for a session with no task: one on a task acts on that
// task. A task of another project, or none, is refused, and nothing opens.
func TestProjectSession_RefusesWhatItCannotReach(t *testing.T) {
	f := newFixture(t)
	f.startFree(t, "pm")
	f.start(t, "peer", "", "", nil)

	in := ports.ReviewRequestInput{Subject: "s", Body: "b"}
	_, err := f.svc.RequestUserReviewOn(ctx, "peer", f.ticket, in)
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.SetTaskStatusOn(ctx, "peer", f.ticket, domain.TicketStatusReview, "")
	wantCode(t, err, "INVALID_INPUT")

	_, err = f.svc.RequestUserReviewOn(ctx, "pm", "", in)
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.RequestUserReviewOn(ctx, "pm", "missing", in)
	wantCode(t, err, "TICKET_NOT_FOUND")
	_, err = f.svc.SetTaskStatusOn(ctx, "pm", "missing", domain.TicketStatusReview, "")
	wantCode(t, err, "TICKET_NOT_FOUND")

	_, err = f.svc.RequestUserReviewOn(ctx, "ghost", f.ticket, in)
	wantCode(t, err, "SESSION_NOT_FOUND")

	if pending, _ := f.svc.ListReviewRequests(ctx, f.ticket, domain.ReviewPending); len(pending) != 0 {
		t.Fatalf("a refused request opened a review: %+v", pending)
	}
}

// A task of another project is out of reach, whoever the session is.
func TestProjectSession_RefusesATaskOfAnotherProject(t *testing.T) {
	f := newFixture(t)
	f.startFree(t, "pm")
	p2, err := f.projects.Create("other", "/tmp/other")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := f.plan.CreateTicket(ctx, p2.ID, "Theirs", "", domain.TicketStatusTodo, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.RequestUserReviewOn(ctx, "pm", theirs.ID, ports.ReviewRequestInput{Subject: "s", Body: "b"})
	wantCode(t, err, "TASK_NOT_ON_PROJECT")
	_, err = f.svc.SetTaskStatusOn(ctx, "pm", theirs.ID, domain.TicketStatusReview, "")
	wantCode(t, err, "TASK_NOT_ON_PROJECT")
}

// A task that has an architect keeps its reviews and its status with it.
func TestProjectSession_DefersToTheTasksArchitect(t *testing.T) {
	f := newFixture(t)
	f.startFree(t, "pm")
	f.start(t, "arch", "", domain.SessionModeArchitect, intp(0))
	_, err := f.svc.RequestUserReviewOn(ctx, "pm", f.ticket, ports.ReviewRequestInput{Subject: "s", Body: "b"})
	wantCode(t, err, "ARCHITECT_ONLY")
	_, err = f.svc.SetTaskStatusOn(ctx, "pm", f.ticket, domain.TicketStatusReview, "")
	wantCode(t, err, "TASK_STATUS_OWNED_BY_ARCHITECT")
}
