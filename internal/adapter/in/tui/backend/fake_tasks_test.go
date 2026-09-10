package backend

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

func TestFakeTicketLifecycle(t *testing.T) {
	f := NewFake()
	f.Projects = []*domain.Project{{ID: "p1"}}
	tk, err := f.CreateTicket("p1", "Fix login", "redirect loops", domain.TicketStatusTodo)
	if err != nil || tk.ID == "" || len(f.Tickets) != 1 {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.CreateTicket("nope", "x", "", domain.TicketStatusTodo); errs.Code(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing project: %v", err)
	}
	if _, err := f.CreateTicket("p1", "", "", domain.TicketStatusTodo); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("empty title: %v", err)
	}
	if _, err := f.UpdateTicket(tk.ID, "Fix login", "redirect loops", domain.TicketStatusReview); err != nil || f.Tickets[0].Status != domain.TicketStatusReview {
		t.Fatalf("update: %v", err)
	}
	if err := f.DeleteTicket(tk.ID); err != nil || len(f.Tickets) != 0 {
		t.Fatalf("delete: %v", err)
	}
	if err := f.DeleteTicket("nope"); errs.Code(err) != "TICKET_NOT_FOUND" {
		t.Fatalf("missing ticket: %v", err)
	}
}

func TestFakeDocumentsLinkedToTickets(t *testing.T) {
	f := NewFake()
	f.Documents = []*domain.Document{{ID: "d1", ProjectID: "p1", Title: "Spec", Content: "# Spec"}, {ID: "d2", ProjectID: "p1", Title: "Notes"}}
	f.TicketDocuments = map[string][]string{"t1": {"d2"}}
	if got := f.ListTicketDocuments("t1"); len(got) != 1 || got[0].ID != "d2" {
		t.Fatalf("ticket documents: %+v", got)
	}
	if got := f.GetDocument("d1"); got == nil || got.Content != "# Spec" {
		t.Fatalf("get document: %+v", got)
	}
	if got := f.ListDocuments("p1"); len(got) != 2 {
		t.Fatalf("list documents: %d", len(got))
	}
}

func TestFakeBranchesAndStart(t *testing.T) {
	f := NewFake()
	f.Projects = []*domain.Project{{ID: "p1"}}
	f.Repositories = []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "main"}}
	f.Branches = map[string][]domain.GitBranch{"r1": {{Name: "main", IsHead: true}, {Name: "origin/dev", Remote: true}}}
	if got, err := f.ListBranches("r1"); err != nil || len(got) != 2 || !got[0].IsHead {
		t.Fatalf("branches: %v %+v", err, got)
	}
	if _, err := f.ListBranches("nope"); errs.Code(err) != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("missing repo: %v", err)
	}
	req := orchestration.StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "do it", Branch: "do-it-ab12", BaseBranch: "main"}
	s, err := f.StartSession(context.Background(), req)
	if err != nil || s == nil || s.Branch != "do-it-ab12" || s.Status != domain.SessionStarting || len(f.Sessions) != 1 {
		t.Fatalf("start: %v %+v", err, s)
	}
	if len(f.Started) != 1 || f.Started[0].Task != "do it" {
		t.Fatalf("start should be recorded: %+v", f.Started)
	}
	if _, err := f.StartSession(context.Background(), orchestration.StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "again", Branch: "do-it-ab12"}); errs.Code(err) != "BRANCH_EXISTS" {
		t.Fatalf("duplicate branch: %v", err)
	}
	if _, err := f.StartSession(context.Background(), orchestration.StartRequest{ProjectID: "p1", Task: "x"}); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("missing repository: %v", err)
	}
}
