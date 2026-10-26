package orchestration

import (
	"context"
	"errors"
	"testing"

	"operators-mcp/internal/domain"
)

// stubBranchNames answers every check with check, and remembers what it was asked.
type stubBranchNames struct {
	check *domain.BranchNameCheck
	asked []string
}

func (s *stubBranchNames) CheckBranchName(_ context.Context, _, name, _ string) (*domain.BranchNameCheck, error) {
	s.asked = append(s.asked, name)
	c := *s.check
	c.Name = name
	return &c, nil
}

func detailsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var de *domain.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("err %v carries no details", err)
	}
	return de.Details.(map[string]string)
}

func TestStartInteractive_ANamedBranchIsTheSessionsBranchExactly(t *testing.T) {
	svc, prov := newInteractiveService(t)
	svc.UseBranchNames(&stubBranchNames{check: &domain.BranchNameCheck{Valid: true, Available: true}})
	sess, _ := startInteractive(t, svc, InteractiveRequest{BranchName: "  my/feature  "})
	if sess.Branch != "my/feature" || prov.created[0].Branch != "my/feature" {
		t.Fatalf("branch = %q / %q, want my/feature", sess.Branch, prov.created[0].Branch)
	}
}

func TestStartInteractive_AnInvalidNamedBranchIsRefusedAndNothingIsCreated(t *testing.T) {
	svc, prov := newInteractiveService(t)
	svc.UseBranchNames(&stubBranchNames{check: &domain.BranchNameCheck{Code: domain.BranchNameInvalid, Reason: "a branch name cannot contain spaces"}})
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", BranchName: "bad name"})
	if codeOf(err) != domain.BranchNameInvalid {
		t.Fatalf("err = %v, want BRANCH_NAME_INVALID", err)
	}
	if d := detailsOf(t, err); d["name"] != "bad name" || d["reason"] == "" {
		t.Fatalf("details = %v", d)
	}
	var se *domain.StructuredError
	errors.As(err, &se)
	if want := "BRANCH_NAME_INVALID: "; se.Message[:len(want)] != want {
		t.Fatalf("message %q does not start with the code", se.Message)
	}
	if len(prov.created) != 0 {
		t.Fatalf("a worktree was made: %v", prov.created)
	}
}

func TestStartInteractive_ANamedBranchThatExistsIsRefusedWithAFreeVariant(t *testing.T) {
	svc, prov := newInteractiveService(t)
	svc.UseBranchNames(&stubBranchNames{check: &domain.BranchNameCheck{Valid: true, Code: domain.BranchExists, Reason: "exists", Suggestion: "feat-2"}})
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", BranchName: "feat"})
	if codeOf(err) != domain.BranchExists {
		t.Fatalf("err = %v, want BRANCH_EXISTS", err)
	}
	if d := detailsOf(t, err); d["name"] != "feat" || d["suggestion"] != "feat-2" {
		t.Fatalf("details = %v", d)
	}
	if len(prov.created) != 0 {
		t.Fatalf("a worktree was made: %v", prov.created)
	}
}

// A name taken after the check and before the worktree is made is refused by
// the worktree's own check, with the same code and a suggestion.
func TestStartInteractive_ANameTakenMeanwhileIsRefusedAtTheWorktree(t *testing.T) {
	svc, prov := newInteractiveService(t)
	prov.createErr = &domain.StructuredError{Code: domain.BranchExists, Message: "branch feat already exists"}
	calls := 0
	svc.UseBranchNames(checkerFunc(func(name string) *domain.BranchNameCheck {
		calls++
		if calls == 1 {
			return &domain.BranchNameCheck{Valid: true, Available: true}
		}
		return &domain.BranchNameCheck{Valid: true, Code: domain.BranchExists, Suggestion: "feat-2"}
	}))
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", BranchName: "feat"})
	if codeOf(err) != domain.BranchExists {
		t.Fatalf("err = %v", err)
	}
	if d := detailsOf(t, err); d["suggestion"] != "feat-2" {
		t.Fatalf("details = %v", d)
	}
}

type checkerFunc func(name string) *domain.BranchNameCheck

func (f checkerFunc) CheckBranchName(_ context.Context, _, name, _ string) (*domain.BranchNameCheck, error) {
	c := *f(name)
	c.Name = name
	return &c, nil
}

func TestStartInteractive_WithoutABranchNameTheNameIsGenerated(t *testing.T) {
	svc, _ := newInteractiveService(t)
	stub := &stubBranchNames{check: &domain.BranchNameCheck{Valid: true, Available: true}}
	svc.UseBranchNames(stub)
	sess, _ := startInteractive(t, svc, InteractiveRequest{BranchName: "   "})
	if want := "agent/ship-the-thing-" + sess.ID[:8]; sess.Branch != want || len(stub.asked) != 0 {
		t.Fatalf("branch = %q (checked %v), want %q unchecked", sess.Branch, stub.asked, want)
	}
}
