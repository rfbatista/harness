package planning_test

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/tool"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const doneIsThePersons = "Only the person moves a task to done. Move it to review when the work is ready for them to check."

func refusedAsThePersons(t *testing.T, err error) {
	t.Helper()
	if code(err) != "TASK_DONE_IS_THE_PERSONS" || err.Error() != "TASK_DONE_IS_THE_PERSONS: "+doneIsThePersons {
		t.Fatalf("want TASK_DONE_IS_THE_PERSONS with the contract's message, got %v", err)
	}
}

// A session moves a task into done by no route of planning: neither a patch
// that names it, nor one made on a session's call, nor a task created in done.
// Nothing of the refused call is applied and nothing is recorded.
func TestTickets_OnlyThePersonMovesATaskToDone(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	history := sqlite.NewTaskStatusChangeRepository(db)
	svc.StatusHistory = history
	person := context.Background()
	session := tool.WithSessionID(person, "sess-1")

	tk, _ := svc.CreateTicket(person, p.ID, "Ship it", "", domain.TicketStatusReview)

	_, err = svc.PatchTicket(person, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusDone), BySessionID: "sess-1"})
	refusedAsThePersons(t, err)
	_, err = svc.PatchTicket(session, tk.ID, ports.TicketPatch{Title: strp("Shipped"), Status: statusp(domain.TicketStatusDone)})
	refusedAsThePersons(t, err)
	_, err = svc.UpdateTicket(session, tk.ID, "Shipped", "", domain.TicketStatusDone)
	refusedAsThePersons(t, err)
	got, _ := svc.GetTicket(person, tk.ID)
	if got.Status != domain.TicketStatusReview || got.Title != "Ship it" {
		t.Fatalf("a refused call changes nothing: %+v", got)
	}
	if h := history.ListByTask(tk.ID); len(h) != 0 {
		t.Fatalf("a refused move is not recorded: %+v", h)
	}

	_, err = svc.CreateTicket(session, p.ID, "Already shipped", "", domain.TicketStatusDone)
	refusedAsThePersons(t, err)
	if list, _ := svc.ListTickets(person, p.ID); len(list) != 1 {
		t.Fatalf("a refused create creates nothing: %d tasks", len(list))
	}

	// The same session may still edit, and move a task anywhere but done.
	if _, err := svc.PatchTicket(session, tk.ID, ports.TicketPatch{Title: strp("Ship it well")}); err != nil {
		t.Fatal(err)
	}

	// The person moves it to done.
	got, err = svc.PatchTicket(person, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusDone)})
	if err != nil || got.Status != domain.TicketStatusDone {
		t.Fatalf("the person moves it to done: %+v %v", got, err)
	}
	// A session takes it back out of done.
	got, err = svc.PatchTicket(session, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusInProgress), BySessionID: "sess-1"})
	if err != nil || got.Status != domain.TicketStatusInProgress {
		t.Fatalf("a session moves it out of done: %+v %v", got, err)
	}
}
