package planning_test

import (
	"context"
	"errors"
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
