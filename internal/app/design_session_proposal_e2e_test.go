//go:build !windows

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A design session submits its draft as one proposal over MCP, and the person
// reads it over the API (contract "Harness server ↔ Agent sessions — design-
// system view, draft and proposals", "Session tool"; "Harness server ↔ Web
// UI — project design system", "Design sessions and proposals").

// withInk is tokens.json with one token more.
func withInk(tokens string) string {
	return strings.Replace(tokens, `"signal"`, `"ink": {"$type": "color", "$value": "#111111"}, "signal"`, 1)
}

// submit calls submit_design_system_proposal as the session and answers the
// tool's text and whether it was an error.
func (e *shellE2E) submit(s runningSession, summary string) (string, bool) {
	e.t.Helper()
	return callTaskRaw(e.t, e.dial(s), "submit_design_system_proposal", map[string]any{"summary": summary})
}

// getDesignSystem reads the design system over the API.
func (e *shellE2E) getDesignSystem(id string) (out struct {
	Version       int `json:"version"`
	OpenProposals int `json:"open_proposals"`
}) {
	e.t.Helper()
	e.post("get_design_system", map[string]any{"design_system_id": id}, &out)
	return out
}

type proposalList struct {
	Proposals []struct {
		ID             string `json:"id"`
		SessionID      string `json:"session_id"`
		Agent          string `json:"agent"`
		SessionRunning bool   `json:"session_running"`
		State          string `json:"state"`
		BaseVersion    int    `json:"base_version"`
		Summary        string `json:"summary"`
		Items          int    `json:"items"`
		Conflicting    int    `json:"conflicting"`
	} `json:"proposals"`
}

func (e *shellE2E) proposals(ds string) proposalList {
	e.t.Helper()
	var out proposalList
	e.post("list_design_system_proposals", map[string]any{"design_system_id": ds}, &out)
	return out
}

type proposalItem struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Op          string         `json:"op"`
	Key         string         `json:"key"`
	Base        map[string]any `json:"base"`
	Current     map[string]any `json:"current"`
	Proposed    map[string]any `json:"proposed"`
	Conflicting bool           `json:"conflicting"`
	Decision    string         `json:"decision"`
}

type proposalView struct {
	ID                 string         `json:"id"`
	State              string         `json:"state"`
	BaseVersion        int            `json:"base_version"`
	CurrentVersion     int            `json:"current_version"`
	Summary            string         `json:"summary"`
	Items              []proposalItem `json:"items"`
	AffectedComponents []struct {
		Slug    string   `json:"slug"`
		ByItems []string `json:"by_items"`
	} `json:"affected_components"`
	ProposedRecord struct {
		Tokens []map[string]any `json:"tokens"`
	} `json:"proposed_record"`
}

func (e *shellE2E) proposal(id string) proposalView {
	e.t.Helper()
	var out proposalView
	e.post("get_design_system_proposal", map[string]any{"proposal_id": id}, &out)
	return out
}

func TestDesignProposal_SubmitIsRefusedWhenEmptyOrNotADesignSessionOrAlreadyOpen(t *testing.T) {
	e := newShellE2E(t)
	id := e.savedDesignSystem()
	task := e.start(e.task("Build the page"))
	if text, isErr := e.submit(task, "x"); !isErr || !strings.Contains(text, "NOT_A_DESIGN_SESSION") {
		t.Fatalf("a task session's submit = %q (error %v)", text, isErr)
	}

	s := e.designSession(id)
	if text, isErr := e.submit(s, "nothing yet"); !isErr || !strings.Contains(text, "PROPOSAL_EMPTY") {
		t.Fatalf("an unchanged draft's submit = %q (error %v)", text, isErr)
	}
	if a := e.draftWrite(s, "tokens.json", withInk(e.designFile(s, "tokens.json"))); !a.Allow {
		t.Fatalf("write: %+v", a)
	}
	if text, isErr := e.submit(s, ""); !isErr || !strings.Contains(text, "summary") {
		t.Fatalf("a submit with no summary = %q (error %v)", text, isErr)
	}
	text, isErr := e.submit(s, "Add the ink colour")
	if isErr {
		t.Fatalf("submit: %s", text)
	}
	var got struct {
		ProposalID  string `json:"proposal_id"`
		Items       int    `json:"items"`
		Conflicting int    `json:"conflicting"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil || got.ProposalID == "" || got.Items != 1 || got.Conflicting != 0 {
		t.Fatalf("submit answered %q: %+v (%v)", text, got, err)
	}
	if text, isErr := e.submit(s, "again"); !isErr || !strings.Contains(text, "PROPOSAL_SUBMITTED") {
		t.Fatalf("a second submit = %q (error %v)", text, isErr)
	}
	// While the proposal waits, the draft takes no writes.
	wantRefused(t, e.draftWrite(s, "DESIGN.md", e.designFile(s, "DESIGN.md")), "PROPOSAL_SUBMITTED", "waits on the person")
}

func TestDesignProposal_TheListAndTheViewShowEachItemWithItsBaseCurrentAndProposedValues(t *testing.T) {
	e := newShellE2E(t)
	id := e.savedDesignSystem()
	s := e.designSession(id)

	// Nothing to decide on yet: an unchanged draft is not listed.
	if got := e.proposals(id); len(got.Proposals) != 0 {
		t.Fatalf("an unchanged draft is listed: %+v", got)
	}

	tokens := e.designFile(s, "tokens.json")
	// Two items: a new token, and a changed value the button's previews use.
	changed := strings.Replace(withInk(tokens), "oklch(0.5 0.095 182)", "oklch(0.4 0.1 200)", 1)
	if changed == tokens {
		t.Fatalf("tokens.json lacks signal:\n%s", tokens)
	}
	if a := e.draftWrite(s, "tokens.json", changed); !a.Allow {
		t.Fatalf("write: %+v", a)
	}
	drafting := e.proposals(id)
	if len(drafting.Proposals) != 1 || drafting.Proposals[0].State != "drafting" || drafting.Proposals[0].Items != 2 {
		t.Fatalf("a changed draft lists as %+v, want drafting with 2 items", drafting)
	}
	if got := e.getDesignSystem(id); got.OpenProposals != 0 {
		t.Fatalf("a draft is not an open proposal: %d", got.OpenProposals)
	}

	if text, isErr := e.submit(s, "Calmer signal, add ink"); isErr {
		t.Fatalf("submit: %s", text)
	}
	list := e.proposals(id)
	if len(list.Proposals) != 1 {
		t.Fatalf("proposals = %+v", list)
	}
	row := list.Proposals[0]
	if row.State != "open" || row.BaseVersion != 1 || row.Items != 2 || row.Conflicting != 0 || row.Summary != "Calmer signal, add ink" || row.SessionID != s.id || !row.SessionRunning {
		t.Fatalf("row = %+v", row)
	}
	if got := e.getDesignSystem(id); got.OpenProposals != 1 {
		t.Fatalf("open_proposals = %d, want 1", got.OpenProposals)
	}
	var open proposalList
	e.post("list_design_system_proposals", map[string]any{"design_system_id": id, "state": "approved"}, &open)
	if len(open.Proposals) != 0 {
		t.Fatalf("a state filter listed %+v", open)
	}

	view := e.proposal(row.ID)
	if view.State != "open" || view.BaseVersion != 1 || view.CurrentVersion != 1 || len(view.Items) != 2 {
		t.Fatalf("view = %+v", view)
	}
	byKey := map[string]proposalItem{}
	for _, it := range view.Items {
		byKey[it.Key] = it
	}
	ink, signal := byKey["colors.ink"], byKey["colors.signal"]
	if ink.Kind != "token" || ink.Op != "add" || ink.Base != nil || ink.Current != nil || ink.Proposed["value"] != "#111111" || ink.Decision != "pending" || ink.Conflicting {
		t.Errorf("ink = %+v", ink)
	}
	if signal.Op != "change" || signal.Base["value"] != "oklch(0.5 0.095 182)" || signal.Current["value"] != "oklch(0.5 0.095 182)" || signal.Proposed["value"] != "oklch(0.4 0.1 200)" || signal.Conflicting {
		t.Errorf("signal = %+v", signal)
	}
	// The button's previews use the signal token: they get before and after.
	if len(view.AffectedComponents) != 1 || view.AffectedComponents[0].Slug != "button" || len(view.AffectedComponents[0].ByItems) != 1 || view.AffectedComponents[0].ByItems[0] != signal.ID {
		t.Errorf("affected = %+v", view.AffectedComponents)
	}
	if len(view.ProposedRecord.Tokens) != 5 {
		t.Errorf("the proposed record has %d tokens, want 5", len(view.ProposedRecord.Tokens))
	}
	if status, text := postJSON(t, e.srv.URL+"/api/get_design_system_proposal", `{"proposal_id":"nope"}`); status != 404 || !strings.Contains(text, "PROPOSAL_NOT_FOUND") {
		t.Errorf("an unknown proposal = %d %s", status, text)
	}
}

func TestDesignProposal_ItemsCoverPrinciplesSectionsAndComponentsAndAChangeSinceTheBaseConflicts(t *testing.T) {
	e := newShellE2E(t)
	id := e.savedDesignSystem()
	s := e.designSession(id)

	design := e.designFile(s, "DESIGN.md")
	if a := e.draftWrite(s, "DESIGN.md", strings.Replace(design, "Quiet surfaces.", "Quiet surfaces, always.\n\n## Motion\n\nMove only to explain.", 1)); !a.Allow {
		t.Fatalf("write DESIGN.md: %+v", a)
	}
	if a := e.draftRemove(s, "components/button.html"); !a.Allow {
		t.Fatalf("rm component: %+v", a)
	}
	if text, isErr := e.submit(s, "Principles, no button"); isErr {
		t.Fatalf("submit: %s", text)
	}
	view := e.proposal(e.proposals(id).Proposals[0].ID)
	keys := map[string]string{}
	for _, it := range view.Items {
		keys[it.Kind+" "+it.Key] = it.Op
	}
	want := map[string]string{"principles_section ## Calm": "change", "principles_section ## Motion": "add", "component button": "remove"}
	if len(keys) != len(want) {
		t.Fatalf("items = %v, want %v", keys, want)
	}
	for k, op := range want {
		if keys[k] != op {
			t.Errorf("item %s = %q, want %q", k, keys[k], op)
		}
	}

	// The person changes the design system meanwhile: the Calm section.
	record := dsRecord(buttonComponent())
	record["principles"] = "# Principles\n\n## Calm\n\nQuiet surfaces, my way.\n"
	e.saveTo(id, 1, record)
	row := e.proposals(id).Proposals[0]
	if row.Conflicting != 1 {
		t.Errorf("conflicting = %d, want only the Calm change: %+v", row.Conflicting, row)
	}
	view = e.proposal(row.ID)
	if view.BaseVersion != 1 || view.CurrentVersion != 2 {
		t.Errorf("versions = %d/%d", view.BaseVersion, view.CurrentVersion)
	}
	for _, it := range view.Items {
		if it.Kind == "principles_section" && it.Key == "## Calm" && !it.Conflicting {
			t.Errorf("the Calm item does not conflict: %+v", it)
		}
		if it.Key == "## Motion" && it.Conflicting {
			t.Errorf("a new section conflicts: %+v", it)
		}
	}
}

func TestDesignProposal_AnOpenProposalIsOnTheFeedAndInTheInbox(t *testing.T) {
	e := newShellE2E(t)
	feed := &archE2E{t: t, ctx: e.ctx, srv: e.srv, project: e.project}
	feed.follow()
	id := e.savedDesignSystem()
	s := e.designSession(id)
	if a := e.draftWrite(s, "tokens.json", withInk(e.designFile(s, "tokens.json"))); !a.Allow {
		t.Fatalf("write: %+v", a)
	}
	text, isErr := e.submit(s, "Add ink")
	if isErr {
		t.Fatalf("submit: %s", text)
	}
	var got struct {
		ProposalID string `json:"proposal_id"`
	}
	_ = json.Unmarshal([]byte(text), &got)
	feed.sawOnFeed("design_system_proposal", `"project_id":"`+e.project+`"`, `"design_system_id":"`+id+`"`, `"proposal_id":"`+got.ProposalID+`"`, `"state":"open"`)

	page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{ProjectID: e.project})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range page.Notifications {
		if strings.Contains(n.Link, "tab=proposals") && strings.Contains(n.Link, got.ProposalID) && n.SessionID == s.id && n.Level == domain.NotificationAction {
			found = true
		}
	}
	if !found {
		t.Fatalf("no action notification attributed to the session links the proposal: %+v", page.Notifications)
	}
}
