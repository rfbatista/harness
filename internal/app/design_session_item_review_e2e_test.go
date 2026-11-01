//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The person decides a proposal item by item (contract "Harness server ↔ Web
// UI — project design system", decide_design_system_proposal; ADR 0055).

// itemsByKey is the proposal's items by their key.
func (e *shellE2E) itemsByKey(proposalID string) map[string]proposalItem {
	e.t.Helper()
	out := map[string]proposalItem{}
	for _, it := range e.proposal(proposalID).Items {
		out[it.Key] = it
	}
	return out
}

func decision(item proposalItem, d string) map[string]string {
	return map[string]string{"item_id": item.ID, "decision": d}
}

// storedTokens are the tokens of the design system's current record by key.
func (e *shellE2E) storedTokens(ds string) map[string]map[string]any {
	e.t.Helper()
	var stored struct {
		Record struct {
			Tokens []map[string]any `json:"tokens"`
		} `json:"record"`
	}
	e.post("get_design_system", map[string]any{"design_system_id": ds}, &stored)
	out := map[string]map[string]any{}
	for _, tok := range stored.Record.Tokens {
		out[tok["group"].(string)+"."+tok["name"].(string)] = tok
	}
	return out
}

func TestDesignProposalItems_AnApprovedItemIsAppliedWhileTheOthersStayPendingUntilDecided(t *testing.T) {
	e := newShellE2E(t)
	ds, _, id := e.openProposal()
	items := e.itemsByKey(id)
	ink, signal := items["colors.ink"], items["colors.signal"]

	status, out := e.decideWith(id, []map[string]string{decision(ink, "approve")}, nil)
	if status != http.StatusOK || out["version"] != float64(2) || out["state"] != "open" {
		t.Fatalf("approve one = %d %v, want version 2, open", status, out)
	}
	tokens := e.storedTokens(ds)
	if tokens["colors.ink"] == nil || tokens["colors.signal"]["value"] != "oklch(0.5 0.095 182)" {
		t.Fatalf("only the ink item should be applied: %v", tokens)
	}
	view := e.itemsByKey(id)
	if view["colors.ink"].Decision != "approved" || view["colors.signal"].Decision != "pending" || view["colors.ink"].Conflicting {
		t.Fatalf("decisions = %+v", view)
	}
	if rows := e.proposals(ds).Proposals; rows[0].State != "open" {
		t.Fatalf("the list says %s, want open", rows[0].State)
	}

	// An item decided once is decided: deciding it again is refused.
	if status, out := e.decideWith(id, []map[string]string{decision(ink, "reject")}, nil); status != http.StatusBadRequest || out["code"] != "INVALID_INPUT" {
		t.Fatalf("deciding an approved item again = %d %v", status, out)
	}

	// The last item, rejected: nothing is written, and the proposal is partly approved.
	status, out = e.decideWith(id, []map[string]string{decision(signal, "reject")}, nil)
	if status != http.StatusOK || out["version"] != nil || out["state"] != "partly_approved" {
		t.Fatalf("reject the rest = %d %v, want no version, partly_approved", status, out)
	}
	if got := e.getDesignSystem(ds); got.Version != 2 {
		t.Fatalf("version = %d, want 2", got.Version)
	}
	view = e.itemsByKey(id)
	if view["colors.ink"].Decision != "approved" || view["colors.signal"].Decision != "rejected" {
		t.Fatalf("outcomes = %+v", view)
	}
	if rows := e.proposals(ds).Proposals; rows[0].State != "partly_approved" {
		t.Fatalf("the list says %s, want partly_approved", rows[0].State)
	}
	if status, out := e.decideWith(id, []map[string]string{decision(signal, "approve")}, nil); status != http.StatusConflict || out["code"] != "PROPOSAL_NOT_OPEN" {
		t.Fatalf("deciding a decided proposal = %d %v", status, out)
	}
}

func TestDesignProposalItems_ApprovingEveryItemInTwoSittingsGivesTheRecordOfApprovingTheWhole(t *testing.T) {
	whole := newShellE2E(t)
	wholeDS, _, wholeID := whole.openProposal()
	if status, out := whole.decide(wholeID, "approve"); status != http.StatusOK || out["state"] != "approved" {
		t.Fatalf("approve all = %d %v", status, out)
	}

	e := newShellE2E(t)
	ds, _, id := e.openProposal()
	items := e.itemsByKey(id)
	if status, out := e.decideWith(id, []map[string]string{decision(items["colors.signal"], "approve")}, nil); status != http.StatusOK || out["state"] != "open" {
		t.Fatalf("first sitting = %d %v", status, out)
	}
	status, out := e.decideWith(id, []map[string]string{decision(items["colors.ink"], "approve")}, nil)
	if status != http.StatusOK || out["state"] != "approved" || out["version"] != float64(3) {
		t.Fatalf("second sitting = %d %v, want approved at version 3", status, out)
	}
	got, want := e.storedTokens(ds), whole.storedTokens(wholeDS)
	for key, tok := range want {
		if got[key]["value"] == nil || got[key]["type"] != tok["type"] || toJSON(got[key]["value"]) != toJSON(tok["value"]) {
			t.Errorf("token %s = %v, want %v", key, got[key], tok)
		}
	}
	if len(got) != len(want) {
		t.Errorf("tokens = %v, want %v", got, want)
	}
}

func TestDesignProposalItems_EachConflictingItemNeedsItsOwnConfirmation(t *testing.T) {
	e := newShellE2E(t)
	ds, _, id := e.openProposal()
	// The person meanwhile changes the signal colour and adds ink themselves.
	record := dsRecord(buttonComponent())
	tokens := record["tokens"].([]any)
	tokens[0] = dsToken("colors", "signal", "color", "oklch(0.6 0.2 30)")
	record["tokens"] = append(tokens, dsToken("colors", "ink", "color", "#222222"))
	e.saveTo(ds, 1, record)
	items := e.itemsByKey(id)
	if !items["colors.signal"].Conflicting || !items["colors.ink"].Conflicting {
		t.Fatalf("both should conflict: %+v", items)
	}
	both := []map[string]string{decision(items["colors.signal"], "approve"), decision(items["colors.ink"], "approve")}
	status, out := e.decideWith(id, both, []string{items["colors.signal"].ID})
	details, _ := out["details"].(map[string]any)
	ids, _ := details["item_ids"].([]any)
	if status != http.StatusConflict || out["code"] != "CONFLICT_NOT_CONFIRMED" || len(ids) != 1 || ids[0] != items["colors.ink"].ID {
		t.Fatalf("one confirmation of two = %d %v, want the ink item named", status, out)
	}
	if got := e.getDesignSystem(ds); got.Version != 2 {
		t.Fatalf("a refused apply wrote version %d", got.Version)
	}
	// A rejected conflicting item needs no confirmation.
	status, out = e.decideWith(id, []map[string]string{decision(items["colors.signal"], "approve"), decision(items["colors.ink"], "reject")}, []string{items["colors.signal"].ID})
	if status != http.StatusOK || out["version"] != float64(3) || out["state"] != "partly_approved" {
		t.Fatalf("approve one confirmed, reject the other = %d %v", status, out)
	}
}

func TestDesignProposalItems_ADraftMadeStaleByAPersonEditStillAppliesItsValidItems(t *testing.T) {
	e := newShellE2E(t)
	ds := e.savedDesignSystem()
	s := e.designSession(ds)
	tokens := e.designFile(s, "tokens.json")
	if a := e.draftWrite(s, "tokens.json", withInk(tokens)); !a.Allow {
		t.Fatalf("write tokens: %+v", a)
	}
	button := e.designFile(s, "components/button.html")
	reworded := strings.Replace(button, "Starts an action.", "Starts one action.", 1)
	if reworded == button {
		t.Fatalf("the button file lacks its purpose:\n%s", button)
	}
	if a := e.draftWrite(s, "components/button.html", reworded); !a.Allow {
		t.Fatalf("write button: %+v", a)
	}
	if text, isErr := e.submit(s, "Ink, a reworded button"); isErr {
		t.Fatalf("submit: %s", text)
	}
	id := e.proposals(ds).Proposals[0].ID

	// The person drops the spacing token and the button's use of it meanwhile.
	record := dsRecord(dsComponent("button", dsPreview(`<button>Save</button>`, `.x{background:var(--colors-signal)}`)))
	var kept []any
	for _, tok := range record["tokens"].([]any) {
		if tok.(map[string]any)["name"] != "md" {
			kept = append(kept, tok)
		}
	}
	record["tokens"] = kept
	e.saveTo(ds, 1, record)
	// The draft still holds the old button, whose previews need spacing.md: it
	// cannot be applied on the current record.
	items := e.itemsByKey(id)
	status, out := e.decideWith(id, []map[string]string{decision(items["button"], "approve")}, []string{items["button"].ID})
	details, _ := out["details"].(map[string]any)
	keys, _ := details["item_keys"].([]any)
	if status != http.StatusBadRequest || out["code"] != "DESIGN_SYSTEM_INVALID" || len(keys) != 1 || keys[0] != "button" {
		t.Fatalf("approve the stale button = %d %v, want DESIGN_SYSTEM_INVALID naming button", status, out)
	}
	if got := e.getDesignSystem(ds); got.Version != 2 {
		t.Fatalf("a refused apply wrote version %d", got.Version)
	}
	if v := e.proposal(id); v.State != "open" {
		t.Fatalf("a refused apply left the proposal %s", v.State)
	}

	// The person rejects the stale item and applies the valid one.
	status, out = e.decideWith(id, []map[string]string{decision(items["button"], "reject"), decision(items["colors.ink"], "approve")}, nil)
	if status != http.StatusOK || out["version"] != float64(3) || out["state"] != "partly_approved" {
		t.Fatalf("reject the stale item, approve ink = %d %v", status, out)
	}
	if e.storedTokens(ds)["colors.ink"] == nil {
		t.Fatal("the valid item was not applied")
	}
}

func TestDesignProposalItems_AComponentNeedsItsTokenApprovedInTheSameApply(t *testing.T) {
	e := newShellE2E(t)
	ds := e.savedDesignSystem()
	s := e.designSession(ds)
	if a := e.draftWrite(s, "tokens.json", withInk(e.designFile(s, "tokens.json"))); !a.Allow {
		t.Fatalf("write tokens: %+v", a)
	}
	chip := strings.NewReplacer("Button", "Chip", "button", "chip", "colors-signal", "colors-ink").Replace(e.designFile(s, "components/button.html"))
	if a := e.draftWrite(s, "components/chip.html", chip); !a.Allow {
		t.Fatalf("write chip: %+v", a)
	}
	if text, isErr := e.submit(s, "Ink and a chip that uses it"); isErr {
		t.Fatalf("submit: %s", text)
	}
	id := e.proposals(ds).Proposals[0].ID
	items := e.itemsByKey(id)
	if items["chip"].ID == "" || items["colors.ink"].ID == "" {
		t.Fatalf("items = %+v", items)
	}

	for name, decisions := range map[string][]map[string]string{
		"token undecided": {decision(items["chip"], "approve")},
		"token rejected":  {decision(items["chip"], "approve"), decision(items["colors.ink"], "reject")},
	} {
		status, out := e.decideWith(id, decisions, nil)
		keys, _ := out["details"].(map[string]any)["item_keys"].([]any)
		if status != http.StatusBadRequest || out["code"] != "DESIGN_SYSTEM_INVALID" || len(keys) != 1 || keys[0] != "chip" {
			t.Errorf("%s: %d %v, want DESIGN_SYSTEM_INVALID naming chip", name, status, out)
		}
	}
	if got := e.getDesignSystem(ds); got.Version != 1 {
		t.Fatalf("a refused apply wrote version %d", got.Version)
	}
	status, out := e.decideWith(id, []map[string]string{decision(items["chip"], "approve"), decision(items["colors.ink"], "approve")}, nil)
	if status != http.StatusOK || out["version"] != float64(2) || out["state"] != "approved" {
		t.Fatalf("approve both = %d %v", status, out)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
