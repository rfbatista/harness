package planning_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func newService(t *testing.T) (*planning.Service, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	return svc, p.ID
}

func newServiceWithProjectRepo(t *testing.T) (*planning.Service, ports.ProjectRepository, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	return svc, projects, p.ID
}

func code(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestCreateTicket_DefaultsAndValidation(t *testing.T) {
	svc, pid := newService(t)

	tk, err := svc.CreateTicket(context.Background(), pid, "T", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != domain.TicketStatusBacklog {
		t.Fatalf("want default backlog, got %q", tk.Status)
	}

	if _, err := svc.CreateTicket(context.Background(), pid, "", "", ""); code(err) != "INVALID_INPUT" {
		t.Fatalf("want INVALID_INPUT, got %v", err)
	}
	if _, err := svc.CreateTicket(context.Background(), "nope", "T", "", ""); code(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("want PROJECT_NOT_FOUND, got %v", err)
	}
	if _, err := svc.CreateTicket(context.Background(), pid, "T", "", domain.TicketStatus("weird")); code(err) != "INVALID_STATUS" {
		t.Fatalf("want INVALID_STATUS, got %v", err)
	}
}

func TestUpdateTicket_EmptyStatusKeepsCurrent(t *testing.T) {
	svc, pid := newService(t)

	// Create a ticket (defaults to backlog).
	tk, err := svc.CreateTicket(context.Background(), pid, "T", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != domain.TicketStatusBacklog {
		t.Fatalf("want initial status backlog, got %q", tk.Status)
	}

	// Update to in_progress with explicit status.
	updated, err := svc.UpdateTicket(context.Background(), tk.ID, "T", "", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != domain.TicketStatusInProgress {
		t.Fatalf("want status in_progress, got %q", updated.Status)
	}

	// Update with empty status should preserve in_progress.
	updated2, err := svc.UpdateTicket(context.Background(), tk.ID, "T renamed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if updated2.Status != domain.TicketStatusInProgress {
		t.Fatalf("want status to stay in_progress, got %q", updated2.Status)
	}
	if updated2.Title != "T renamed" {
		t.Fatalf("want title 'T renamed', got %q", updated2.Title)
	}

	// Update with empty status on missing ticket should return TICKET_NOT_FOUND.
	if _, err := svc.UpdateTicket(context.Background(), "missing-id", "T", "", ""); code(err) != "TICKET_NOT_FOUND" {
		t.Fatalf("want TICKET_NOT_FOUND, got %v", err)
	}
}

func TestLinkDocument_CrossProjectRejected(t *testing.T) {
	svc, pid := newService(t)

	tk, _ := svc.CreateTicket(context.Background(), pid, "T", "", "")
	doc, _ := svc.CreateDocument(pid, "D", "body")
	if err := svc.LinkDocument(tk.ID, doc.ID); err != nil {
		t.Fatalf("same-project link should succeed: %v", err)
	}
	if got := svc.ListTicketDocuments(tk.ID); len(got) != 1 {
		t.Fatalf("want 1 linked doc, got %d", len(got))
	}

	// A document in a different project must be rejected.
	otherDoc, err := svc.CreateDocument("nope", "X", "")
	if code(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("want PROJECT_NOT_FOUND creating doc in missing project, got %v", err)
	}
	_ = otherDoc

	if code(svc.LinkDocument("missing", doc.ID)) != "TICKET_NOT_FOUND" {
		t.Fatal("want TICKET_NOT_FOUND")
	}
	if code(svc.LinkDocument(tk.ID, "missing")) != "DOCUMENT_NOT_FOUND" {
		t.Fatal("want DOCUMENT_NOT_FOUND")
	}

	// Exercise the CROSS_PROJECT_ACCESS guard with a genuine second project.
	svc2, projects, p1id := newServiceWithProjectRepo(t)
	p2, err := projects.Create("proj2", "/tmp/proj2")
	if err != nil {
		t.Fatal(err)
	}
	tk2, err := svc2.CreateTicket(context.Background(), p1id, "T2", "", "")
	if err != nil {
		t.Fatalf("failed to create ticket in project 1: %v", err)
	}
	doc2, err := svc2.CreateDocument(p2.ID, "D2", "body2")
	if err != nil {
		t.Fatalf("failed to create document in project 2: %v", err)
	}
	// Ticket tk2 is in p1id, document doc2 is in p2.ID (different project).
	if code(svc2.LinkDocument(tk2.ID, doc2.ID)) != "CROSS_PROJECT_ACCESS" {
		t.Fatal("want CROSS_PROJECT_ACCESS for cross-project link")
	}
}

func strp(s string) *string                              { return &s }
func statusp(s domain.TicketStatus) *domain.TicketStatus { return &s }

// A patch changes only the fields it carries: the kanban board's status-only
// move and the task page's text-only edit both leave the rest alone.
func TestPatchTicket_OnlyPresentFieldsChange(t *testing.T) {
	svc, pid := newService(t)
	tk, err := svc.CreateTicket(context.Background(), pid, "Ship it", "with care", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}

	moved, err := svc.PatchTicket(context.Background(), tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusReview)})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Status != domain.TicketStatusReview || moved.Title != "Ship it" || moved.Description != "with care" {
		t.Fatalf("status-only patch touched the text: %+v", moved)
	}

	edited, err := svc.PatchTicket(context.Background(), tk.ID, ports.TicketPatch{Title: strp("Ship it now"), Description: strp("")})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Title != "Ship it now" || edited.Description != "" || edited.Status != domain.TicketStatusReview {
		t.Fatalf("text patch moved the card or kept the description: %+v", edited)
	}
}

// The same status again is a no-op: the ticket comes back as stored, with its
// updated_at untouched.
func TestPatchTicket_SameStatusIsIdempotent(t *testing.T) {
	svc, pid := newService(t)
	tk, _ := svc.CreateTicket(context.Background(), pid, "T", "", domain.TicketStatusInProgress)

	again, err := svc.PatchTicket(context.Background(), tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusInProgress)})
	if err != nil {
		t.Fatal(err)
	}
	if !again.UpdatedAt.Equal(tk.UpdatedAt) || again.Status != tk.Status {
		t.Fatalf("no-op patch wrote: before %+v after %+v", tk, again)
	}
}

func TestPatchTicket_Validation(t *testing.T) {
	svc, pid := newService(t)
	tk, _ := svc.CreateTicket(context.Background(), pid, "T", "d", domain.TicketStatusTodo)
	ctx := context.Background()

	if _, err := svc.PatchTicket(ctx, "", ports.TicketPatch{}); code(err) != "INVALID_INPUT" {
		t.Fatalf("missing id: want INVALID_INPUT, got %v", err)
	}
	if _, err := svc.PatchTicket(ctx, "missing", ports.TicketPatch{Status: statusp(domain.TicketStatusDone)}); code(err) != "TICKET_NOT_FOUND" {
		t.Fatalf("unknown id: want TICKET_NOT_FOUND, got %v", err)
	}
	if _, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Title: strp("")}); code(err) != "INVALID_INPUT" {
		t.Fatalf("blank title: want INVALID_INPUT, got %v", err)
	}
	if _, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Title: strp("   ")}); code(err) != "INVALID_INPUT" {
		t.Fatalf("whitespace title: want INVALID_INPUT, got %v", err)
	}
	if _, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatus("weird"))}); code(err) != "INVALID_INPUT" {
		t.Fatalf("bad status: want INVALID_INPUT, got %v", err)
	}
	// A present-but-empty status keeps the current one: the TUI client sends
	// "status": "" whenever it has none.
	kept, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: statusp("")})
	if err != nil || kept.Status != domain.TicketStatusTodo {
		t.Fatalf("empty status: want todo kept, got %+v, %v", kept, err)
	}
	// Nothing above may have written.
	got, _ := svc.GetTicket(ctx, tk.ID)
	if got.Title != "T" || got.Description != "d" || got.Status != domain.TicketStatusTodo {
		t.Fatalf("a rejected patch wrote: %+v", got)
	}
}

// The full-field update every existing caller sends is the patch with every
// field present. Its invalid-status code follows the HTTP contract.
func TestUpdateTicket_IsTheFullPatch(t *testing.T) {
	svc, pid := newService(t)
	tk, _ := svc.CreateTicket(context.Background(), pid, "T", "d", domain.TicketStatusTodo)

	got, err := svc.UpdateTicket(context.Background(), tk.ID, "T2", "", domain.TicketStatusDone)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "T2" || got.Description != "" || got.Status != domain.TicketStatusDone {
		t.Fatalf("full update = %+v", got)
	}
	if _, err := svc.UpdateTicket(context.Background(), tk.ID, "T2", "", domain.TicketStatus("weird")); code(err) != "INVALID_INPUT" {
		t.Fatalf("bad status: want INVALID_INPUT, got %v", err)
	}
	if _, err := svc.UpdateTicket(context.Background(), tk.ID, "", "", ""); code(err) != "INVALID_INPUT" {
		t.Fatalf("blank title: want INVALID_INPUT, got %v", err)
	}
}

// recorder is a ports.TicketAnnouncer that keeps what it was told, in order.
type recorder struct {
	mu      sync.Mutex
	changes []recorded
}

type recorded struct {
	ticket  domain.Ticket
	deleted bool
}

func (r *recorder) AnnounceTicket(tk *domain.Ticket, deleted bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changes = append(r.changes, recorded{ticket: *tk, deleted: deleted})
}

func (r *recorder) all() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.changes...)
}

// Every create, update and delete is announced with the ticket as it is after
// the change; a patch that changes nothing is not.
func TestTickets_ChangesAreAnnounced(t *testing.T) {
	svc, pid := newService(t)
	rec := &recorder{}
	svc.Announcer = rec
	ctx := context.Background()

	tk, err := svc.CreateTicket(ctx, pid, "Ship it", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusInProgress)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusInProgress)}); err != nil {
		t.Fatal(err) // no-op
	}
	if _, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Title: strp("")}); code(err) != "INVALID_INPUT" {
		t.Fatal(err) // rejected
	}
	if _, err := svc.UpdateTicket(ctx, tk.ID, "Ship it now", "d", domain.TicketStatusReview); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteTicket(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}

	got := rec.all()
	if len(got) != 4 {
		t.Fatalf("announced %d changes, want create, patch, update, delete: %+v", len(got), got)
	}
	if got[0].ticket.ID != tk.ID || got[0].ticket.Status != domain.TicketStatusTodo || got[0].deleted {
		t.Fatalf("create announced as %+v", got[0])
	}
	if got[1].ticket.Status != domain.TicketStatusInProgress || got[1].deleted {
		t.Fatalf("patch announced as %+v", got[1])
	}
	if got[2].ticket.Title != "Ship it now" || got[2].ticket.Status != domain.TicketStatusReview {
		t.Fatalf("update announced as %+v", got[2])
	}
	if !got[3].deleted || got[3].ticket.ID != tk.ID || got[3].ticket.ProjectID != pid {
		t.Fatalf("delete announced as %+v", got[3])
	}
}

// With no announcer (tests, the MCP-only server) nothing is announced and
// nothing panics.
func TestTickets_NoAnnouncerIsFine(t *testing.T) {
	svc, pid := newService(t)
	tk, err := svc.CreateTicket(context.Background(), pid, "T", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PatchTicket(context.Background(), tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusDone)}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteTicket(context.Background(), tk.ID); err != nil {
		t.Fatal(err)
	}
}

// Messages for one ticket arrive in the order the changes were applied: the
// last one announced is what is stored. Run with -race.
func TestTickets_ConcurrentPatchesAnnounceInOrder(t *testing.T) {
	svc, pid := newService(t)
	rec := &recorder{}
	svc.Announcer = rec
	ctx := context.Background()
	tk, err := svc.CreateTicket(ctx, pid, "T", "", domain.TicketStatusBacklog)
	if err != nil {
		t.Fatal(err)
	}
	statuses := []domain.TicketStatus{domain.TicketStatusTodo, domain.TicketStatusInProgress, domain.TicketStatusReview, domain.TicketStatusDone}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := statuses[i%len(statuses)]
			if _, err := svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: &st}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	stored, _ := svc.GetTicket(ctx, tk.ID)
	got := rec.all()
	if last := got[len(got)-1]; last.ticket.Status != stored.Status {
		t.Fatalf("last announced %s, stored %s", last.ticket.Status, stored.Status)
	}
}
