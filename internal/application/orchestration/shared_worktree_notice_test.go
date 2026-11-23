package orchestration

import (
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The opening of a session on a shared worktree names who else is there
// (Contract: Harness server ↔ Agent sessions — shared worktree notices).

var sharedPeers = []sharedPeer{
	{Agent: "go-developer", Mode: domain.SessionModeImplement, Access: domain.AccessWrite, Running: true},
	{Agent: "reviewer", Mode: domain.SessionModeReview, Access: domain.AccessReadOnly, Running: false},
}

func TestSharedWorktreeSection_NamesTheBranchThePathAndEveryOtherSession(t *testing.T) {
	golden(t, "shared_worktree_section", sharedWorktreeSection("agent/export-1a2b", "/work/export", domain.AccessWrite, sharedPeers))
}

func TestSharedWorktreeSection_AReadOnlySessionIsToldItCannotWriteAndWhereToReport(t *testing.T) {
	golden(t, "shared_worktree_section_read_only", sharedWorktreeSection("agent/export-1a2b", "/work/export", domain.AccessReadOnly, sharedPeers))
}

func TestSharedWorktreeSection_ASessionAloneGetsNothing(t *testing.T) {
	if got := sharedWorktreeSection("agent/export-1a2b", "/work/export", domain.AccessWrite, nil); got != "" {
		t.Errorf("section for a session alone = %q", got)
	}
}

func TestWithSharedWorktree_ASessionAloneKeepsItsOpeningByteForByte(t *testing.T) {
	svc, _ := newInteractiveService(t)
	cfg := llmkit.SessionConfig{AppendSystem: "be terse\n\n## Your task\n\nx"}
	before := cfg.AppendSystem
	svc.withSharedWorktree(t.Context(), &cfg, "s1", "ws1", domain.AccessWrite, "b", "/w")
	if cfg.AppendSystem != before {
		t.Errorf("opening changed to %q", cfg.AppendSystem)
	}
	svc.withSharedWorktree(t.Context(), &cfg, "s1", "", domain.AccessWrite, "", "")
	if cfg.AppendSystem != before {
		t.Errorf("a session with no worktree got %q", cfg.AppendSystem)
	}
}

func TestSharedWorktreeOpening_AJoinerIsToldWhoIsThereAndALoneCreatorIsNot(t *testing.T) {
	svc, _, creator := joinWorld(t)
	// The creator started alone: nothing about sharing.
	if got := svc.sessions.Get(creator.ID); got == nil {
		t.Fatal("creator not stored")
	}
	_, alone, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(alone.AppendSystem, "## Shared worktree") {
		t.Errorf("a session alone got the section:\n%s", alone.AppendSystem)
	}

	sess, spec := joinAs(t, svc, creator, "implement", "")
	sys := spec.AppendSystem
	for _, want := range []string{"## Shared worktree", creator.Branch, creator.WorkingDir, "implement", "access write", "running", "Coordinate through commits"} {
		if !strings.Contains(sys, want) {
			t.Errorf("opening of the joiner lacks %q:\n%s", want, sys)
		}
	}
	if strings.Contains(sys, sess.ID) {
		t.Errorf("the session lists itself:\n%s", sys)
	}
	if strings.Contains(sys, "read-only access: you cannot write") {
		t.Errorf("a writer was told it cannot write:\n%s", sys)
	}
}

func TestSharedWorktreeOpening_AReadOnlyJoinerIsToldItCannotWrite(t *testing.T) {
	svc, _, creator := joinWorld(t)
	_, spec := joinAs(t, svc, creator, "review", "")
	if !strings.Contains(spec.AppendSystem, "You cannot write this worktree") || !strings.Contains(spec.AppendSystem, "task documents or comments") {
		t.Errorf("opening:\n%s", spec.AppendSystem)
	}
}

func TestSharedWorktreeOpening_AnEndedSessionIsListedAsEnded(t *testing.T) {
	svc, _, creator := joinWorld(t)
	if _, err := svc.EndInteractive(t.Context(), creator.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec := joinAs(t, svc, creator, "review", "")
	if !strings.Contains(spec.AppendSystem, "ended") || strings.Contains(spec.AppendSystem, "running") {
		t.Errorf("opening:\n%s", spec.AppendSystem)
	}
}

func TestSharedWorktreeOpening_AResumeListsWhoIsThereNowNotAtTheFirstStart(t *testing.T) {
	svc, _, creator := joinWorld(t)
	if _, err := svc.EndInteractive(t.Context(), creator.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: creator.ID})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spec.AppendSystem, "## Shared worktree") {
		t.Fatalf("resume while alone got the section:\n%s", spec.AppendSystem)
	}
	if _, err := svc.EndInteractive(t.Context(), creator.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	joiner, _ := joinAs(t, svc, creator, "review", "")
	_, spec, err = svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: creator.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec.AppendSystem, "## Shared worktree") || !strings.Contains(spec.AppendSystem, "review") || strings.Contains(spec.AppendSystem, creator.ID) || strings.Contains(spec.AppendSystem, joiner.ID) {
		t.Errorf("resumed opening:\n%s", spec.AppendSystem)
	}
}
