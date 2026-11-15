//go:build !windows

package app

import (
	"net/http"
	"testing"
)

// Approval commits the files and the proposal's state together: a failure
// after the saves leaves no file saved and the proposal open (spec story 12).
func TestInstructionProposal_AFailureAfterTheSavesLeavesNothingSavedAndTheProposalOpen(t *testing.T) {
	e := newShellE2E(t)
	s, got := e.submitted()
	if err := e.db.Exec(`CREATE TRIGGER fail_approval BEFORE UPDATE ON instruction_proposals
		WHEN NEW.state = 'approved' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}

	status, out := e.decideProposal("approve", got.ProposalID, nil)
	if status == http.StatusOK {
		t.Fatalf("approve succeeded despite the failure: %v", out)
	}
	if _, _, ok := e.installed(skillPrefix + "verify-harness/SKILL.md"); ok {
		t.Error("a file was saved although the approval failed")
	}
	if v, c, _ := e.installed(skillPrefix + "features-harness/SKILL.md"); v != 1 || c != featuresSkill {
		t.Errorf("a changed file was saved: v%d %q", v, c)
	}
	if _, _, ok := e.installed(skillPrefix + "features-harness/old-feature.md"); !ok {
		t.Error("a file was removed although the approval failed")
	}
	if rows, open := e.listProposals("open"); len(rows) != 1 || open != 1 {
		t.Fatalf("the proposal is not open: %+v", rows)
	}

	// Once the failure is gone the same approval goes through: it was never
	// refused as stale.
	if err := e.db.Exec(`DROP TRIGGER fail_approval`).Error; err != nil {
		t.Fatal(err)
	}
	if status, out := e.decideProposal("approve", got.ProposalID, nil); status != http.StatusOK {
		t.Fatalf("approve after the failure = %d %v", status, out)
	}
	if v, _, ok := e.installed(skillPrefix + "verify-harness/SKILL.md"); !ok || v != 1 {
		t.Errorf("the approved skill = v%d %v", v, ok)
	}
	_ = s
}
