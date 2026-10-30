package orchestration

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// stubBases records each base it is asked to ready, with how many worktrees
// existed by then, and refuses with err.
type stubBases struct {
	err      error
	asked    []string
	provSeen []int
	prov     *fakeProvisioner
}

func (s *stubBases) PrepareBase(_ context.Context, repositoryID, _, base string) error {
	s.asked = append(s.asked, repositoryID+"|"+base)
	s.provSeen = append(s.provSeen, len(s.prov.created))
	return s.err
}

func TestStartInteractive_ABaseIsReadiedBeforeTheWorktreeIsCut(t *testing.T) {
	svc, prov := newInteractiveService(t)
	bases := &stubBases{prov: prov}
	svc.UseBaseBranches(bases)
	startInteractive(t, svc, InteractiveRequest{BaseBranch: "origin/feature-x"})
	if len(bases.asked) != 1 || bases.asked[0] != "r1|origin/feature-x" {
		t.Fatalf("asked %v", bases.asked)
	}
	if bases.provSeen[0] != 0 || len(prov.created) != 1 || prov.baseRefs[0] != "origin/feature-x" {
		t.Fatalf("worktrees at the check: %d, cut with %v", bases.provSeen[0], prov.baseRefs)
	}
}

func TestStartInteractive_ARefusedBaseCreatesNothing(t *testing.T) {
	for _, code := range []string{domain.BaseBranchNotFound, domain.RemoteBranchNotFound, domain.CodeNoRemote, domain.CodeFetchFailed, domain.CodeGitCredentialMissing} {
		t.Run(code, func(t *testing.T) {
			svc, prov := newInteractiveService(t)
			svc.UseBaseBranches(&stubBases{prov: prov, err: &domain.StructuredError{Code: code, Message: "no"}})
			_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", BaseBranch: "origin/feature-x"})
			if codeOf(err) != code {
				t.Fatalf("err = %v, want %s", err, code)
			}
			if len(prov.created) != 0 {
				t.Fatalf("a worktree was made: %v", prov.created)
			}
			if sessions := svc.sessions.List(context.Background(), ports.SessionFilter{}); len(sessions) != 0 {
				t.Fatalf("a session row was made: %v", sessions)
			}
		})
	}
}
