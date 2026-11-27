package orchestration

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// Protection follows the worktree, not the mode (ADR 0012): a session that
// writes in a gated session's worktree has the same protected paths in its
// kept configuration and its sandbox policy, and the owner's scope, never more.

func joinedGuardWorld(t *testing.T, creatorScope ...string) (*Service, *domain.Session, *portstest.RecordingSandboxDriver, memGuard) {
	t.Helper()
	svc, _ := newInteractiveService(t)
	driver := &portstest.RecordingSandboxDriver{}
	svc.Sandbox = &Sandbox{Driver: driver, Home: t.TempDir()}
	guards := memGuard{}
	svc.GuardSessions = guards
	creator, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI, Scope: creatorScope})
	if err != nil {
		t.Fatal(err)
	}
	return svc, creator, driver, guards
}

func TestAWriterJoiningAGatedWorktreeIsProtectedWhateverItsMode(t *testing.T) {
	for _, mode := range []string{"design", "operate", "review", "qa", "product", "architect", ""} {
		t.Run("mode "+mode, func(t *testing.T) {
			svc, creator, driver, guards := joinedGuardWorld(t)
			before := len(driver.Calls())
			joiner, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{
				ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: mode, Access: "write",
				RunsOn: domain.RunnerTUI, WorktreeID: creator.WorkspaceID})
			if err != nil {
				t.Skipf("a %q session cannot join with write access: %v", mode, err)
			}
			cfg := guards[joiner.ID]
			if !cfg.WorktreeGated || !cfg.ProtectsPaths() || len(cfg.ProtectedPaths) == 0 {
				t.Fatalf("the joiner's kept configuration does not protect: %+v", cfg)
			}
			calls := driver.Calls()
			if len(calls) <= before {
				t.Skip("the joiner's start is not sandboxed in this world")
			}
			policy := calls[len(calls)-1].Policy
			if !slices.Contains(policy.Readable, filepath.Join(joiner.WorkingDir, "check.sh")) {
				t.Errorf("the joiner's sandbox does not make check.sh read-only: %v", policy.Readable)
			}
		})
	}
}

func TestAJoinerGetsTheOwnersScopeAndNeverMore(t *testing.T) {
	svc, creator, driver, guards := joinedGuardWorld(t, "check.sh")
	joiner, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "operate", Access: "write",
		RunsOn: domain.RunnerTUI, WorktreeID: creator.WorkspaceID,
		Scope: []string{".github/workflows/**"}}) // its own request for more is ignored
	if err != nil {
		t.Fatal(err)
	}
	cfg := guards[joiner.ID]
	if !slices.Equal(cfg.Scope, []string{"check.sh"}) {
		t.Errorf("the joiner's scope = %v, want the owner's [check.sh] and nothing it asked for", cfg.Scope)
	}
	policy := driver.Calls()[len(driver.Calls())-1].Policy
	if slices.Contains(policy.Readable, filepath.Join(joiner.WorkingDir, "check.sh")) {
		t.Error("the owner's scope-unlocked path is read-only for the joiner")
	}
	if !slices.Contains(policy.Readable, filepath.Join(joiner.WorkingDir, ".github", "workflows")) {
		t.Error("the joiner's own scope request unlocked another protected path")
	}
}

func TestAJoinerOfAWorktreeThatIsNotGatedIsNotProtectedAndAResumeKeepsTheProtection(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Sandbox = &Sandbox{Driver: &portstest.RecordingSandboxDriver{}, Home: t.TempDir()}
	guards := memGuard{}
	svc.GuardSessions = guards
	owner, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "design", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	joiner, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "operate", Access: "write", RunsOn: domain.RunnerTUI, WorktreeID: owner.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if guards[joiner.ID].WorktreeGated {
		t.Error("a joiner of a worktree whose owner is not gated was protected")
	}

	svc2, creator, _, guards2 := joinedGuardWorld(t)
	gj, _, err := svc2.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "operate", Access: "write", RunsOn: domain.RunnerTUI, WorktreeID: creator.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc2.sessions.UpdateStatus(gj.ID, domain.SessionDone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc2.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: gj.ID}); err != nil {
		t.Fatal(err)
	}
	if !guards2[gj.ID].WorktreeGated {
		t.Error("a resume dropped the joiner's protection")
	}
}
