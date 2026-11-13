//go:build !windows

package app

import (
	"strings"
	"testing"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The branch instruction a session starts with follows its mode's branch
// block at that moment (ADR 0088; contract "Harness server ↔ Agent sessions —
// branch gate" §6).

// branchInstruction starts an interactive session of mode and returns the
// branch instruction it received.
func branchInstruction(t *testing.T, e *shellE2E, mode domain.SessionMode, task string) string {
	t.Helper()
	e.orch.SessionBranchGateURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/branch_gate" }
	_, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task(task), Mode: string(mode)})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range spec.InstructionFiles {
		if f.Path == orchestration.BranchGateInstructionPath {
			return string(f.Content)
		}
	}
	t.Fatal("the session has no branch instruction")
	return ""
}

func TestASessionStartedWhileItsModeIsBlockedIsToldTheBranchesAreBlocked(t *testing.T) {
	e := newShellE2E(t)
	got := branchInstruction(t, e, domain.SessionModeImplement, "Blocked at start")
	for _, want := range []string{
		"Stay on your session branch", "BRANCH_ACTION_BLOCKED", "Creating, switching and deleting branches is blocked",
		"rename waits for the person", "push", "Force pushes and remote branch deletions are refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the blocked instruction misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Ask for another branch") {
		t.Errorf("the blocked instruction invites a branch request:\n%s", got)
	}
}

func TestASessionStartedWhileItsModeIsNotBlockedKeepsTodaysInstruction(t *testing.T) {
	e := newShellE2E(t)
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{domain.BranchBlockKey(domain.SessionModeImplement): "off"}); err != nil {
		t.Fatal(err)
	}
	got := branchInstruction(t, e, domain.SessionModeImplement, "Not blocked at start")
	want := "# Branches\n\n" +
		"- Stay on your session branch. Do not create, switch, rename or delete branches.\n" +
		"- Ask for another branch only when it is really needed. Run the git command as its own Bash command, so the harness can ask the person. It waits until the person answers.\n" +
		"- Every branch action waits for the person's approval, and an approval covers that one action only. If the person denies it, their note says why.\n" +
		"- A push waits for the person's approval too, whatever the branch or the remote, your session branch included (ADR 0071).\n" +
		"- Force pushes and remote branch deletions are refused.\n"
	if got != want {
		t.Errorf("instruction =\n%s\nwant today's:\n%s", got, want)
	}
}

func TestEachModesSessionGetsTheVariantOfItsOwnKey(t *testing.T) {
	e := newShellE2E(t)
	// Only the architect's block is off.
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{domain.BranchBlockKey(domain.SessionModeArchitect): "off"}); err != nil {
		t.Fatal(err)
	}
	if got := branchInstruction(t, e, domain.SessionModeArchitect, "Architect session"); strings.Contains(got, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("the architect (block off) got the blocked variant:\n%s", got)
	}
	if got := branchInstruction(t, e, domain.SessionModeImplement, "Implement session"); !strings.Contains(got, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("the implement session (block on) got today's text:\n%s", got)
	}
}

func TestALaterChangeOfTheBlockDoesNotRewriteARunningSessionsInstruction(t *testing.T) {
	e := newShellE2E(t)
	e.orch.SessionBranchGateURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/branch_gate" }
	_, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Running"), Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	var started []byte
	for _, f := range spec.InstructionFiles {
		if f.Path == orchestration.BranchGateInstructionPath {
			started = f.Content
		}
	}
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{domain.BranchBlockKey(domain.SessionModeImplement): "off"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range spec.InstructionFiles {
		if f.Path == orchestration.BranchGateInstructionPath && string(f.Content) != string(started) {
			t.Error("the delivered instruction changed after the setting did")
		}
	}
	if !strings.Contains(string(started), "BRANCH_ACTION_BLOCKED") {
		t.Errorf("the session did not start with the blocked variant:\n%s", started)
	}
}
