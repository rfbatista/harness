package taskchannel_test

import (
	"errors"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const doneIsThePersons = "Only the person moves a task to done. Move it to review when the work is ready for them to check."

// wantDoneRefused checks the refusal, and that the task kept its status and
// recorded no move.
func (f *fixture) wantDoneRefused(t *testing.T, by string, was domain.TicketStatus) {
	t.Helper()
	_, err := f.svc.SetTaskStatus(ctx, by, domain.TicketStatusDone, "finished")
	var se *domain.StructuredError
	if errs.Code(err) != "TASK_DONE_IS_THE_PERSONS" || !errors.As(err, &se) || se.Message != doneIsThePersons {
		t.Fatalf("%s asking for done: want TASK_DONE_IS_THE_PERSONS with the contract's message, got %v", by, err)
	}
	tk, _ := f.plan.GetTicket(ctx, f.ticket)
	if tk.Status != was {
		t.Fatalf("%s: the task moved to %s", by, tk.Status)
	}
	if h := f.history.ListByTask(f.ticket); len(h) != 0 {
		t.Fatalf("%s: a refused move is recorded: %+v", by, h)
	}
}

// No session moves a task into done, whatever its role; done wins over the
// architect owning the status.
func TestDoneIsThePersons_EveryRole(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, intp(0))
	f.start(t, "dev", "arch", "", intp(0))
	f.start(t, "peer", "", "", nil)

	f.wantDoneRefused(t, "arch", domain.TicketStatusTodo)
	f.wantDoneRefused(t, "dev", domain.TicketStatusTodo)
	f.wantDoneRefused(t, "peer", domain.TicketStatusTodo)

	// The architect still moves it anywhere else.
	if _, err := f.svc.SetTaskStatus(ctx, "arch", domain.TicketStatusReview, "ready"); err != nil {
		t.Fatal(err)
	}
}

func TestDoneIsThePersons_TaskWithNoArchitect(t *testing.T) {
	f := newFixture(t)
	f.start(t, "peer", "", "", nil)
	f.wantDoneRefused(t, "peer", domain.TicketStatusTodo)
}

// A task the person moved to done can be reopened by a session.
func TestDoneIsThePersons_SessionsMoveATaskOutOfDone(t *testing.T) {
	f := newFixture(t)
	f.start(t, "peer", "", "", nil)
	if _, err := f.plan.PatchTicket(ctx, f.ticket, ports.TicketPatch{Status: ptr(domain.TicketStatusDone)}); err != nil {
		t.Fatalf("the person moves it to done: %v", err)
	}
	tk, err := f.svc.SetTaskStatus(ctx, "peer", domain.TicketStatusInProgress, "reopened")
	if err != nil || tk.Status != domain.TicketStatusInProgress {
		t.Fatalf("a session moves it out of done: %+v %v", tk, err)
	}
}
