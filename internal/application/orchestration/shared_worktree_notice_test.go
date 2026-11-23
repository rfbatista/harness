package orchestration

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
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
	if strings.Contains(sys, "You cannot write this worktree") {
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

// Turns: a session that starts on a worktree is announced once to every other
// running session on it.

func joinTurns(t *testing.T) (*Service, *domain.Session, *recordingDelivery) {
	t.Helper()
	svc, _, creator := joinWorld(t)
	d := &recordingDelivery{}
	svc.deliverUpdate = d.deliver
	return svc, creator, d
}

func TestWorktreeJoined_TheSessionAlreadyThereGetsOneTurnNamingWhoJoinedAndWithWhatAccess(t *testing.T) {
	svc, creator, d := joinTurns(t)
	joiner, _ := joinAs(t, svc, creator, "review", "")
	got := d.list()
	if len(got) != 1 {
		t.Fatalf("turns = %v, want one", got)
	}
	want := creator.ID + "|worktree-joined:" + joiner.ID + "|[worktree · joined] claude (mode review, access read-only) joined your worktree on " + creator.Branch + ". It only reads: nothing changes in the worktree because of it."
	if got[0] != want {
		t.Errorf("turn = %q\nwant   %q", got[0], want)
	}
}

func TestWorktreeJoined_AWriterJoinsAndTheTurnSaysFilesMayChange(t *testing.T) {
	svc, creator, d := joinTurns(t)
	joinAs(t, svc, creator, "implement", "")
	got := d.list()
	if len(got) != 1 || !strings.Contains(got[0], "(mode implement, access write)") || !strings.Contains(got[0], "expect them to change under you") {
		t.Errorf("turns = %v", got)
	}
}

func TestWorktreeJoined_EveryOtherRunningSessionGetsOneAndAnEndedSessionNone(t *testing.T) {
	svc, creator, d := joinTurns(t)
	second, _ := joinAs(t, svc, creator, "implement", "")
	ended, _ := joinAs(t, svc, creator, "test", "")
	if _, err := svc.EndInteractive(t.Context(), ended.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	third, _ := joinAs(t, svc, creator, "review", "")
	var toCreator, toSecond, toEnded, toThird int
	for _, g := range d.list() {
		if !strings.Contains(g, "|worktree-joined:"+third.ID+"|") {
			continue
		}
		switch strings.SplitN(g, "|", 2)[0] {
		case creator.ID:
			toCreator++
		case second.ID:
			toSecond++
		case ended.ID:
			toEnded++
		case third.ID:
			toThird++
		}
	}
	if toCreator != 1 || toSecond != 1 || toEnded != 0 || toThird != 0 {
		t.Errorf("turns for the third join: creator %d second %d ended %d joiner %d, want 1 1 0 0", toCreator, toSecond, toEnded, toThird)
	}
}

func TestWorktreeJoined_ADelegateJoiningItsArchitectsWorktreeSendsNothingToTheArchitect(t *testing.T) {
	svc, creator, d := joinTurns(t)
	other, _ := joinAs(t, svc, creator, "implement", "")
	d.mu.Lock()
	d.got = nil
	d.mu.Unlock()
	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement",
		RunsOn: domain.RunnerTUI, WorktreeID: creator.WorkspaceID, ParentSessionID: creator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := d.list()
	if len(got) != 1 || !strings.HasPrefix(got[0], other.ID+"|") {
		t.Errorf("turns = %v, want one, to the other session only", got)
	}
}

func TestWorktreeJoined_ASessionThatCutsItsOwnWorktreeSendsNothing(t *testing.T) {
	svc, _, d := joinTurns(t)
	if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	if got := d.list(); len(got) != 0 {
		t.Errorf("turns = %v", got)
	}
}

// A session that ends or is stopped is announced once to every other running
// session on its worktree.

func leftTurnsTo(d *recordingDelivery, id string) []string {
	var out []string
	for _, g := range d.list() {
		if strings.HasPrefix(g, id+"|worktree-left:") {
			out = append(out, g)
		}
	}
	return out
}

func TestWorktreeLeft_ASessionThatEndsOrIsStoppedIsAnnouncedOnceToTheOthers(t *testing.T) {
	for name, closedByUser := range map[string]bool{"ended": false, "stopped": true} {
		t.Run(name, func(t *testing.T) {
			svc, creator, d := joinTurns(t)
			joiner, _ := joinAs(t, svc, creator, "review", "")
			if _, err := svc.EndInteractive(t.Context(), joiner.ID, 0, closedByUser); err != nil {
				t.Fatal(err)
			}
			got := leftTurnsTo(d, creator.ID)
			want := creator.ID + "|worktree-left:" + joiner.ID + "|[worktree · left] claude (mode review) left your worktree on " + creator.Branch + "."
			if len(got) != 1 || got[0] != want {
				t.Errorf("turns = %q\nwant one: %q", got, want)
			}
			if own := leftTurnsTo(d, joiner.ID); len(own) != 0 {
				t.Errorf("the session that left was told: %q", own)
			}
		})
	}
}

func TestWorktreeLeft_ASessionThatLeavesTwoWaysIsAnnouncedOnce(t *testing.T) {
	svc, creator, d := joinTurns(t)
	joiner, _ := joinAs(t, svc, creator, "review", "")
	if _, err := svc.EndInteractive(t.Context(), joiner.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.endHeadless(joiner.ID); err != nil {
		t.Fatal(err)
	}
	if got := leftTurnsTo(d, creator.ID); len(got) != 1 {
		t.Errorf("turns = %q, want one", got)
	}
}

func TestWorktreeLeft_ASessionThatResumedAndLeavesAgainIsAnnouncedAgain(t *testing.T) {
	svc, creator, d := joinTurns(t)
	joiner, _ := joinAs(t, svc, creator, "review", "")
	if _, err := svc.EndInteractive(t.Context(), joiner.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: joiner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndInteractive(t.Context(), joiner.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	if got := leftTurnsTo(d, creator.ID); len(got) != 2 {
		t.Errorf("turns = %q, want two", got)
	}
}

func TestWorktreeLeft_EndedSessionsAndASessionAloneAreToldNothing(t *testing.T) {
	svc, creator, d := joinTurns(t)
	ended, _ := joinAs(t, svc, creator, "test", "")
	if _, err := svc.EndInteractive(t.Context(), ended.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	joiner, _ := joinAs(t, svc, creator, "review", "")
	if _, err := svc.EndInteractive(t.Context(), joiner.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := leftTurnsTo(d, ended.ID); len(got) != 0 {
		t.Errorf("an ended session was told: %q", got)
	}
	// The creator is now alone: its own end tells no one.
	before := len(d.list())
	if _, err := svc.EndInteractive(t.Context(), creator.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if len(d.list()) != before {
		t.Errorf("a session alone sent turns: %q", d.list()[before:])
	}
}

func TestSharedWorktreeOpening_PeersAreListedOldestFirst(t *testing.T) {
	svc, creator, _ := joinTurns(t)
	joinAs(t, svc, creator, "test", "")
	joinAs(t, svc, creator, "review", "")
	_, spec := joinAs(t, svc, creator, "qa", "")
	sys := spec.AppendSystem
	implement, test, review := strings.Index(sys, "(mode implement"), strings.Index(sys, "(mode test"), strings.Index(sys, "(mode review")
	if implement < 0 || !(implement < test && test < review) {
		t.Errorf("peers out of order (implement %d, test %d, review %d):\n%s", implement, test, review, sys)
	}
}

func TestWorktreeLeft_AFailedResumeDoesNotRearmTheLeaveAnnouncement(t *testing.T) {
	svc := newHostedService(t, scriptAgent{err: errors.New("no claude")})
	svc.Sandbox = &Sandbox{Driver: &portstest.RecordingSandboxDriver{}, Home: t.TempDir()}
	d := &recordingDelivery{}
	svc.deliverUpdate = d.deliver
	creator, err := startOn(t, svc, InteractiveRequest{Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	joiner, err := startOn(t, svc, InteractiveRequest{Mode: "implement", WorktreeID: creator.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndInteractive(t.Context(), joiner.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	if got := leftTurnsTo(d, creator.ID); len(got) != 1 {
		t.Fatalf("turns after the end = %q, want one", got)
	}
	if _, _, err := svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: joiner.ID, RunsOn: domain.RunnerServer}); err == nil {
		t.Fatal("the resume did not fail")
	}
	if got := leftTurnsTo(d, creator.ID); len(got) != 1 {
		t.Errorf("turns after the failed resume = %q, want still one", got)
	}
}

func TestWorktreeLeft_AProcessThatExitsRightAfterAResumeIsAnnounced(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: "exit 0"})
	svc.Sandbox = &Sandbox{Driver: &portstest.RecordingSandboxDriver{}, Home: t.TempDir()}
	d := &recordingDelivery{}
	svc.deliverUpdate = d.deliver
	creator, err := startOn(t, svc, InteractiveRequest{Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	joiner, err := startOn(t, svc, InteractiveRequest{Mode: "implement", WorktreeID: creator.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndInteractive(t.Context(), joiner.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: joiner.ID, RunsOn: domain.RunnerServer}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(leftTurnsTo(d, creator.ID)) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := leftTurnsTo(d, creator.ID); len(got) != 2 {
		t.Errorf("turns = %q, want the first end and the exit after the resume", got)
	}
}
