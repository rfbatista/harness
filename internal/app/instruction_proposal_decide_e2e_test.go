//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// The person approves, rejects or sends back a proposal as a whole, and the
// proposing session is told (contracts "Harness server ↔ Web UI —
// instruction-file proposals" and "… ↔ Agent sessions — …" §3).

func (e *shellE2E) decideProposal(verb, id string, extra map[string]any) (int, map[string]any) {
	e.t.Helper()
	body := map[string]any{"proposal_id": id}
	for k, v := range extra {
		body[k] = v
	}
	b, _ := json.Marshal(body)
	status, text := postJSON(e.t, e.srv.URL+"/api/"+verb+"_instruction_proposal", string(b))
	var out map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	return status, out
}

// installed is the saved instruction file of the e2e repository, nil when none.
func (e *shellE2E) installed(path string) (version int, content string, found bool) {
	f, err := e.cat.RepositoryArchitectures.GetInstructionFile(e.ctx, e.repo, path)
	if err != nil || f == nil {
		return 0, "", false
	}
	return f.Version, f.Content, true
}

func TestInstructionProposal_ApprovingSavesEveryFileAsANewVersionAndTellsTheSession(t *testing.T) {
	e := newShellE2E(t)
	feed := &archE2E{t: t, ctx: e.ctx, srv: e.srv, project: e.project}
	feed.follow()
	s, got := e.submitted()

	status, out := e.decideProposal("approve", got.ProposalID, nil)
	if status != http.StatusOK {
		t.Fatalf("approve = %d %v", status, out)
	}
	if p, _ := out["proposal"].(map[string]any); p["state"] != "approved" || p["decided_at"] == nil {
		t.Fatalf("proposal = %v", out["proposal"])
	}
	if files, _ := out["files"].([]any); len(files) != 3 { // the removed one is gone
		t.Fatalf("files = %v", out["files"])
	}

	if v, c, ok := e.installed(skillPrefix + "verify-harness/SKILL.md"); !ok || v != 1 || c != verifySkill {
		t.Errorf("created skill = v%d %q %v", v, c, ok)
	}
	if v, c, ok := e.installed(skillPrefix + "features-harness/SKILL.md"); !ok || v != 2 || !strings.Contains(c, "all of them") {
		t.Errorf("changed skill = v%d %q %v", v, c, ok)
	}
	if _, _, ok := e.installed(skillPrefix + "features-harness/old-feature.md"); ok {
		t.Error("the removed file is still saved")
	}
	if v, _, ok := e.installed(skillPrefix + "features-harness/kept.md"); !ok || v != 1 {
		t.Errorf("an identical file was saved again: v%d %v", v, ok)
	}

	feed.sawOnFeed("instruction_file_changed", `"path":"`+skillPrefix+`verify-harness/SKILL.md"`, `"version":1`)
	feed.sawOnFeed("instruction_file_changed", `"path":"`+skillPrefix+`features-harness/old-feature.md"`, `"version":0`)
	feed.sawOnFeed("instruction_proposal_changed", `"proposal_id":"`+got.ProposalID+`"`, `"state":"approved"`, `"open_count":0`)

	turn := nextTurn(t, e.orch, s.id)
	for _, want := range []string{"[instruction files · proposal approved · proposal " + got.ProposalID + "]", skillPrefix + "verify-harness/SKILL.md", "version 1", skillPrefix + "features-harness/SKILL.md", "version 2", "removed"} {
		if !strings.Contains(turn, want) {
			t.Errorf("the approved turn lacks %q:\n%s", want, turn)
		}
	}
	if ans := e.writeDraft(s, draftPath("verify-harness", "SKILL.md"), verifySkill+"more\n"); !ans.Allow {
		t.Errorf("the draft stays read-only after approval: %+v", ans)
	}
	if rows, open := e.listProposals("approved"); len(rows) != 1 || open != 0 {
		t.Errorf("approved = %+v open %d", rows, open)
	}
	page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{ProjectID: e.project})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, note := range page.Notifications {
		if strings.HasPrefix(note.Title, "Instruction files") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d instruction notifications, want only the submit's", n)
	}
	if status, out := e.decideProposal("approve", got.ProposalID, nil); status != http.StatusConflict || out["code"] != "PROPOSAL_DECIDED" {
		t.Errorf("a second approve = %d %v", status, out)
	}
}

func TestInstructionProposal_AnApprovalIsRefusedWhenThePersonSavedAFileSinceAndSavesNothing(t *testing.T) {
	e := newShellE2E(t)
	_, got := e.submitted()
	changed := skillPrefix + "features-harness/SKILL.md"
	if _, err := e.cat.RepositoryArchitectures.SaveInstructionFile(e.ctx, e.repo, changed, "skill", 1, featuresSkill+"The person's edit.\n"); err != nil {
		t.Fatal(err)
	}
	status, out := e.decideProposal("approve", got.ProposalID, nil)
	details, _ := out["details"].(map[string]any)
	paths, _ := details["paths"].([]any)
	if status != http.StatusConflict || out["code"] != "INSTRUCTION_CHANGED" || len(paths) != 1 || paths[0] != changed {
		t.Fatalf("approve = %d %v", status, out)
	}
	if _, _, ok := e.installed(skillPrefix + "verify-harness/SKILL.md"); ok {
		t.Error("a refused approval saved a file")
	}
	if _, _, ok := e.installed(skillPrefix + "features-harness/old-feature.md"); !ok {
		t.Error("a refused approval removed a file")
	}
	if rows, open := e.listProposals("open"); len(rows) != 1 || open != 1 {
		t.Errorf("the proposal is not open: %+v", rows)
	}
}

func TestInstructionProposal_RejectingKeepsItListedAndTellsTheSessionTheNote(t *testing.T) {
	e := newShellE2E(t)
	s, got := e.submitted()
	status, out := e.decideProposal("reject", got.ProposalID, map[string]any{"note": "Too broad"})
	if p, _ := out["proposal"].(map[string]any); status != http.StatusOK || p["state"] != "rejected" || p["note"] != "Too broad" {
		t.Fatalf("reject = %d %v", status, out)
	}
	turn := nextTurn(t, e.orch, s.id)
	if !strings.Contains(turn, "[instruction files · proposal rejected · proposal "+got.ProposalID+"]") || !strings.Contains(turn, "Too broad") {
		t.Errorf("the rejected turn:\n%s", turn)
	}
	if _, _, ok := e.installed(skillPrefix + "verify-harness/SKILL.md"); ok {
		t.Error("a rejection saved a file")
	}
	if rows, open := e.listProposals("rejected"); len(rows) != 1 || open != 0 {
		t.Errorf("rejected = %+v open %d", rows, open)
	}
	if ans := e.writeDraft(s, draftPath("verify-harness", "SKILL.md"), verifySkill+"more\n"); !ans.Allow {
		t.Errorf("the draft stays read-only after a rejection: %+v", ans)
	}
	if status, out := e.decideProposal("reject", got.ProposalID, nil); status != http.StatusConflict || out["code"] != "PROPOSAL_DECIDED" {
		t.Errorf("rejecting a decided proposal = %d %v", status, out)
	}
}

func TestInstructionProposal_ARejectionWithoutANoteSaysNoneInTheTurn(t *testing.T) {
	e := newShellE2E(t)
	s, got := e.submitted()
	if status, out := e.decideProposal("reject", got.ProposalID, nil); status != http.StatusOK {
		t.Fatalf("reject = %d %v", status, out)
	}
	turn := nextTurn(t, e.orch, s.id)
	if !strings.Contains(turn, "proposal rejected") || strings.Contains(turn, "note") {
		t.Errorf("the rejected turn:\n%s", turn)
	}
}

func TestInstructionProposal_SendingBackNeedsANoteReturnsTheDraftAndAllowsANewProposal(t *testing.T) {
	e := newShellE2E(t)
	s, got := e.submitted()
	if status, out := e.decideProposal("send_back", got.ProposalID, map[string]any{"note": "  "}); status != http.StatusBadRequest || out["code"] != "INVALID_INPUT" {
		t.Fatalf("send back without a note = %d %v", status, out)
	}
	status, out := e.decideProposal("send_back", got.ProposalID, map[string]any{"note": "Add the launch steps"})
	if p, _ := out["proposal"].(map[string]any); status != http.StatusOK || p["state"] != "sent_back" || p["note"] != "Add the launch steps" {
		t.Fatalf("send back = %d %v", status, out)
	}
	turn := nextTurn(t, e.orch, s.id)
	for _, want := range []string{"[instruction files · proposal sent back · proposal " + got.ProposalID + "]", "Add the launch steps", skillPrefix + "verify-harness/SKILL.md", skillPrefix + "features-harness/old-feature.md"} {
		if !strings.Contains(turn, want) {
			t.Errorf("the sent back turn lacks %q:\n%s", want, turn)
		}
	}
	if _, _, ok := e.installed(skillPrefix + "verify-harness/SKILL.md"); ok {
		t.Error("a send back saved a file")
	}
	eventually(t, "the draft is writable again", func() error {
		idx, err := shellIndex(s.shell)
		if err != nil {
			return err
		}
		if !idx[draftPath("verify-harness", "SKILL.md")].Writable {
			return &stringError{"still read-only"}
		}
		return nil
	})
	if ans := e.writeDraft(s, draftPath("verify-harness", "launch.md"), "# Launch\n\n1. Build.\n"); !ans.Allow {
		t.Fatalf("a write after the send back = %+v", ans)
	}
	text, isErr := e.propose(s, "harness", []string{"verify-harness", "features-harness"}, "Second try")
	if isErr {
		t.Fatalf("a new proposal after the send back: %s", text)
	}
	rows, open := e.listProposals("")
	if len(rows) != 2 || open != 1 || rows[0].State != "open" || rows[1].State != "sent_back" {
		t.Errorf("proposals = %+v open %d", rows, open)
	}
	if status, out := e.decideProposal("send_back", got.ProposalID, map[string]any{"note": "again"}); status != http.StatusConflict || out["code"] != "PROPOSAL_DECIDED" {
		t.Errorf("sending back a decided proposal = %d %v", status, out)
	}
}
