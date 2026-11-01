//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The person approves or rejects a proposal as a whole; approval writes one
// version, and the design session is told and starts again from it (contract
// "Harness server ↔ Web UI — project design system", "Design sessions and
// proposals"; "Harness server ↔ Agent sessions — …", "Turns to a design
// session").

// openProposal is a design session with a draft that adds the ink token and
// changes the signal colour, submitted.
func (e *shellE2E) openProposal() (ds string, s runningSession, proposalID string) {
	e.t.Helper()
	ds = e.savedDesignSystem()
	s = e.designSession(ds)
	tokens := e.designFile(s, "tokens.json")
	changed := strings.Replace(withInk(tokens), "oklch(0.5 0.095 182)", "oklch(0.4 0.1 200)", 1)
	if a := e.draftWrite(s, "tokens.json", changed); !a.Allow {
		e.t.Fatalf("write: %+v", a)
	}
	text, isErr := e.submit(s, "Calmer signal, add ink")
	if isErr {
		e.t.Fatalf("submit: %s", text)
	}
	var got struct {
		ProposalID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		e.t.Fatal(err)
	}
	return ds, s, got.ProposalID
}

// decide posts decide_design_system_proposal for every item of the proposal.
func (e *shellE2E) decide(proposalID, decision string, confirm ...string) (int, map[string]any) {
	e.t.Helper()
	var decisions []map[string]string
	for _, it := range e.proposal(proposalID).Items {
		decisions = append(decisions, map[string]string{"item_id": it.ID, "decision": decision})
	}
	return e.decideWith(proposalID, decisions, confirm)
}

func (e *shellE2E) decideWith(proposalID string, decisions []map[string]string, confirm []string) (int, map[string]any) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"proposal_id": proposalID, "decisions": decisions, "confirm_overwrite": confirm})
	status, text := postJSON(e.t, e.srv.URL+"/api/decide_design_system_proposal", string(body))
	var out map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	return status, out
}

func (e *shellE2E) versions(ds string) []map[string]any {
	e.t.Helper()
	var out struct {
		Versions []map[string]any `json:"versions"`
	}
	e.post("list_design_system_versions", map[string]any{"design_system_id": ds}, &out)
	return out.Versions
}

func TestDesignProposal_ApprovingWritesOneVersionAndTheSessionStartsAgainFromIt(t *testing.T) {
	e := newShellE2E(t)
	ds, s, id := e.openProposal()

	status, out := e.decide(id, "approve")
	if status != http.StatusOK || out["state"] != "approved" || out["version"] != float64(2) {
		t.Fatalf("approve = %d %v", status, out)
	}
	versions := e.versions(ds)
	if len(versions) != 2 || versions[0]["version"] != float64(2) || versions[0]["source"] != "proposal" || versions[0]["proposal_id"] != id || versions[0]["message"] != "Calmer signal, add ink" {
		t.Fatalf("versions = %v", versions)
	}
	var stored struct {
		Version   int    `json:"version"`
		UpdatedBy string `json:"updated_by"`
		Record    struct {
			Tokens []map[string]any `json:"tokens"`
		} `json:"record"`
	}
	e.post("get_design_system", map[string]any{"design_system_id": ds}, &stored)
	if stored.Version != 2 || stored.UpdatedBy != "proposal "+id || len(stored.Record.Tokens) != 5 {
		t.Fatalf("design system = %+v", stored)
	}
	view := e.proposal(id)
	if view.State != "approved" || view.Items[0].Decision != "approved" {
		t.Errorf("view = %+v", view)
	}
	if got := e.getDesignSystem(ds); got.OpenProposals != 0 {
		t.Errorf("open_proposals = %d", got.OpenProposals)
	}

	turn := nextTurn(t, e.orch, s.id)
	for _, want := range []string{"[design system · proposal decided · proposal " + id + "]", "approved 2, rejected 0", "version 2"} {
		if !strings.Contains(turn, want) {
			t.Errorf("the decided turn lacks %q:\n%s", want, turn)
		}
	}

	// The draft starts again from the new current version: it shows what was
	// approved, and the session may make a second proposal.
	eventually(t, "the draft follows the new version", func() error {
		return fileWith(s.shell, designSystemDir+"/tokens.json", `"ink"`)
	})
	if a := e.draftWrite(s, "DESIGN.md", strings.Replace(e.designFile(s, "DESIGN.md"), "Quiet surfaces.", "Quieter surfaces.", 1)); !a.Allow {
		t.Fatalf("write after the decision: %+v", a)
	}
	if text, isErr := e.submit(s, "Quieter"); isErr {
		t.Fatalf("second submit: %s", text)
	}
	if rows := e.proposals(ds).Proposals; len(rows) != 2 || rows[0].BaseVersion != 2 || rows[0].State != "open" || rows[1].State != "approved" {
		t.Fatalf("proposals = %+v", rows)
	}
}

func TestDesignProposal_RejectingChangesNothingAndKeepsTheProposalListedAsRejected(t *testing.T) {
	e := newShellE2E(t)
	ds, s, id := e.openProposal()
	status, out := e.decide(id, "reject")
	if status != http.StatusOK || out["state"] != "rejected" || out["version"] != nil {
		t.Fatalf("reject = %d %v", status, out)
	}
	if got := e.getDesignSystem(ds); got.Version != 1 || got.OpenProposals != 0 {
		t.Fatalf("design system = %+v", got)
	}
	if rows := e.proposals(ds).Proposals; len(rows) != 1 || rows[0].State != "rejected" {
		t.Fatalf("proposals = %+v", rows)
	}
	turn := nextTurn(t, e.orch, s.id)
	for _, want := range []string{"approved 0, rejected 2", "rejected: colors.ink", "rejected: colors.signal"} {
		if !strings.Contains(turn, want) {
			t.Errorf("the decided turn lacks %q:\n%s", want, turn)
		}
	}
	// The draft is the current version again, without the rejected change.
	eventually(t, "the draft resets", func() error {
		b := e.designFile(s, "tokens.json")
		if strings.Contains(b, `"ink"`) {
			return &stringError{"tokens.json still has ink"}
		}
		return nil
	})
}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

func TestDesignProposal_AConflictingItemNeedsItsOverwriteConfirmed(t *testing.T) {
	e := newShellE2E(t)
	ds, _, id := e.openProposal()

	// The person changes the signal colour meanwhile.
	record := dsRecord(buttonComponent())
	record["tokens"].([]any)[0] = dsToken("colors", "signal", "color", "oklch(0.6 0.2 30)")
	e.saveTo(ds, 1, record)

	conflicting := ""
	for _, it := range e.proposal(id).Items {
		if it.Conflicting {
			conflicting = it.ID
		}
	}
	if conflicting == "" {
		t.Fatal("no item conflicts with the person's save")
	}
	status, out := e.decide(id, "approve")
	details, _ := out["details"].(map[string]any)
	ids, _ := details["item_ids"].([]any)
	if status != http.StatusConflict || out["code"] != "CONFLICT_NOT_CONFIRMED" || len(ids) != 1 || ids[0] != conflicting {
		t.Fatalf("approve without the confirmation = %d %v", status, out)
	}
	if got := e.getDesignSystem(ds); got.Version != 2 {
		t.Fatalf("a refused approval wrote version %d", got.Version)
	}
	if v := e.proposal(id); v.State != "open" {
		t.Fatalf("a refused approval left the proposal %s", v.State)
	}

	status, out = e.decide(id, "approve", conflicting)
	if status != http.StatusOK || out["version"] != float64(3) {
		t.Fatalf("approve with the confirmation = %d %v", status, out)
	}
	var stored struct {
		Record struct {
			Tokens []map[string]any `json:"tokens"`
		} `json:"record"`
	}
	e.post("get_design_system", map[string]any{"design_system_id": ds}, &stored)
	for _, tok := range stored.Record.Tokens {
		if tok["name"] == "signal" && tok["value"] != "oklch(0.4 0.1 200)" {
			t.Errorf("the confirmed overwrite did not apply: %v", tok)
		}
	}
}

func TestDesignProposal_OnlyAnOpenProposalIsDecidedAndOnlyAsAWhole(t *testing.T) {
	e := newShellE2E(t)
	ds := e.savedDesignSystem()
	s := e.designSession(ds)
	if a := e.draftWrite(s, "tokens.json", withInk(e.designFile(s, "tokens.json"))); !a.Allow {
		t.Fatalf("write: %+v", a)
	}
	// A draft is not decidable.
	draft := e.proposals(ds).Proposals[0]
	if status, out := e.decideWith(draft.ID, []map[string]string{{"item_id": "x", "decision": "approve"}}, nil); status != http.StatusConflict || out["code"] != "PROPOSAL_NOT_OPEN" {
		t.Fatalf("deciding a draft = %d %v", status, out)
	}
	if status, out := postJSON(t, e.srv.URL+"/api/decide_design_system_proposal", `{"proposal_id":"nope","decisions":[]}`); status != http.StatusNotFound || !strings.Contains(out, "PROPOSAL_NOT_FOUND") {
		t.Fatalf("an unknown proposal = %d %s", status, out)
	}

	changed := strings.Replace(e.designFile(s, "tokens.json"), "oklch(0.5 0.095 182)", "oklch(0.4 0.1 200)", 1)
	if a := e.draftWrite(s, "tokens.json", changed); !a.Allow {
		t.Fatalf("write: %+v", a)
	}
	text, isErr := e.submit(s, "Ink and a calmer signal")
	if isErr {
		t.Fatalf("submit: %s", text)
	}
	id := draft.ID
	items := e.proposal(id).Items
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	for name, decisions := range map[string][]map[string]string{
		"some items":  {{"item_id": items[0].ID, "decision": "approve"}},
		"mixed":       {{"item_id": items[0].ID, "decision": "approve"}, {"item_id": items[1].ID, "decision": "reject"}},
		"unknown":     {{"item_id": "token:nope", "decision": "approve"}, {"item_id": items[1].ID, "decision": "approve"}},
		"no decision": {{"item_id": items[0].ID, "decision": "maybe"}, {"item_id": items[1].ID, "decision": "approve"}},
		"none":        nil,
	} {
		if status, out := e.decideWith(id, decisions, nil); status != http.StatusBadRequest || out["code"] != "INVALID_INPUT" {
			t.Errorf("%s: %d %v, want 400 INVALID_INPUT", name, status, out)
		}
	}
	if v := e.proposal(id); v.State != "open" {
		t.Fatalf("a refused decision left the proposal %s", v.State)
	}
	if status, out := e.decide(id, "reject"); status != http.StatusOK {
		t.Fatalf("reject = %d %v", status, out)
	}
	if status, out := e.decide(id, "approve"); status != http.StatusConflict || out["code"] != "PROPOSAL_NOT_OPEN" {
		t.Fatalf("deciding twice = %d %v", status, out)
	}
}

func TestDesignProposal_AProposalWhoseSessionEndedIsWithdrawn(t *testing.T) {
	e := newShellE2E(t)
	ds, s, id := e.openProposal()
	body := `{"proposal_id":"` + id + `"}`
	if status, out := postJSON(t, e.srv.URL+"/api/withdraw_design_system_proposal", body); status != http.StatusConflict || !strings.Contains(out, "SESSION_RUNNING") {
		t.Fatalf("withdrawing while the session runs = %d %s", status, out)
	}
	if _, err := e.orch.EndInteractive(e.ctx, s.id, 0, true); err != nil {
		t.Fatal(err)
	}
	status, out := postJSON(t, e.srv.URL+"/api/withdraw_design_system_proposal", body)
	if status != http.StatusOK || !strings.Contains(out, `"withdrawn"`) {
		t.Fatalf("withdraw = %d %s", status, out)
	}
	if rows := e.proposals(ds).Proposals; len(rows) != 1 || rows[0].State != "withdrawn" || rows[0].SessionRunning {
		t.Fatalf("proposals = %+v", rows)
	}
	if got := e.getDesignSystem(ds); got.OpenProposals != 0 || got.Version != 1 {
		t.Fatalf("design system = %+v", got)
	}
	if status, out := postJSON(t, e.srv.URL+"/api/withdraw_design_system_proposal", body); status != http.StatusConflict || !strings.Contains(out, "PROPOSAL_NOT_OPEN") {
		t.Fatalf("withdrawing twice = %d %s", status, out)
	}
}
