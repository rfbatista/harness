//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The person lists and opens the proposals of a repository (contract "Harness
// server ↔ Web UI — instruction-file proposals").

type proposalRow struct {
	ID           string   `json:"id"`
	RepositoryID string   `json:"repository_id"`
	State        string   `json:"state"`
	Summary      string   `json:"summary"`
	Skills       []string `json:"skills"`
	Files        int      `json:"files"`
	Task         struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"task"`
	Session struct {
		ID    string `json:"id"`
		Agent string `json:"agent"`
		Mode  string `json:"mode"`
	} `json:"session"`
	CreatedAt string  `json:"created_at"`
	DecidedAt *string `json:"decided_at"`
	Note      *string `json:"note"`
}

type proposalFile struct {
	Path           string  `json:"path"`
	Op             string  `json:"op"`
	BaseVersion    int     `json:"base_version"`
	CurrentVersion int     `json:"current_version"`
	Stale          bool    `json:"stale"`
	BaseContent    *string `json:"base_content"`
	Content        *string `json:"content"`
}

func (e *shellE2E) listProposals(state string) (rows []proposalRow, open int) {
	e.t.Helper()
	var out struct {
		Proposals []proposalRow `json:"proposals"`
		OpenCount int           `json:"open_count"`
	}
	body := map[string]any{"repository_id": e.repo}
	if state != "" {
		body["state"] = state
	}
	e.post("list_instruction_proposals", body, &out)
	return out.Proposals, out.OpenCount
}

func (e *shellE2E) getProposal(id string) (proposalRow, []proposalFile) {
	e.t.Helper()
	var out struct {
		Proposal proposalRow    `json:"proposal"`
		Files    []proposalFile `json:"files"`
	}
	e.post("get_instruction_proposal", map[string]any{"proposal_id": id}, &out)
	return out.Proposal, out.Files
}

func TestInstructionProposalAPI_ListsAndOpensAProposalWithEachFilesBaseAndContent(t *testing.T) {
	e := newShellE2E(t)
	s, got := e.submitted()

	rows, open := e.listProposals("")
	if len(rows) != 1 || open != 1 {
		t.Fatalf("list = %+v open %d", rows, open)
	}
	r := rows[0]
	if r.ID != got.ProposalID || r.RepositoryID != e.repo || r.State != "open" || r.Summary != "Verification skill and its feature map" ||
		strings.Join(r.Skills, ",") != "verify-harness,features-harness" || r.Files != 4 || r.Task.Title != "Verification skill" ||
		r.Session.ID != s.id || r.Session.Mode != "implement" || r.CreatedAt == "" || r.DecidedAt != nil || r.Note != nil {
		t.Fatalf("row = %+v", r)
	}
	if rows, open := e.listProposals("approved"); len(rows) != 0 || open != 1 {
		t.Fatalf("approved filter = %+v open %d", rows, open)
	}

	_, files := e.getProposal(got.ProposalID)
	byPath := map[string]proposalFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	created := byPath[skillPrefix+"verify-harness/launch.md"]
	if created.Op != "create" || created.BaseVersion != 0 || created.BaseContent != nil || created.Content == nil || *created.Content != "# Launch\n" || created.Stale {
		t.Errorf("create = %+v", created)
	}
	changed := byPath[skillPrefix+"features-harness/SKILL.md"]
	if changed.Op != "change" || changed.BaseVersion != 1 || changed.CurrentVersion != 1 || changed.Stale || changed.BaseContent == nil || *changed.BaseContent != featuresSkill || !strings.Contains(*changed.Content, "all of them") {
		t.Errorf("change = %+v", changed)
	}
	removed := byPath[skillPrefix+"features-harness/old-feature.md"]
	if removed.Op != "remove" || removed.Content != nil || removed.BaseContent == nil || *removed.BaseContent != "stale\n" {
		t.Errorf("remove = %+v", removed)
	}
	if _, kept := byPath[skillPrefix+"features-harness/kept.md"]; kept || len(files) != 4 {
		t.Errorf("files = %+v", files)
	}
}

func TestInstructionProposalAPI_AFileThePersonSavedSinceIsStale(t *testing.T) {
	e := newShellE2E(t)
	_, got := e.submitted()
	if _, err := e.cat.RepositoryArchitectures.SaveInstructionFile(e.ctx, e.repo, skillPrefix+"features-harness/SKILL.md", "skill", 1, featuresSkill+"The person's edit.\n"); err != nil {
		t.Fatal(err)
	}
	_, files := e.getProposal(got.ProposalID)
	for _, f := range files {
		if f.Path == skillPrefix+"features-harness/SKILL.md" && (!f.Stale || f.CurrentVersion != 2 || f.BaseVersion != 1) {
			t.Errorf("file = %+v", f)
		}
		if f.Path == skillPrefix+"verify-harness/SKILL.md" && f.Stale {
			t.Errorf("an unrelated file is stale: %+v", f)
		}
	}
}

func TestInstructionProposalAPI_UnknownProposalAndRepositoryAre404(t *testing.T) {
	e := newShellE2E(t)
	for path, body := range map[string]string{
		"get_instruction_proposal":   `{"proposal_id":"nope"}`,
		"list_instruction_proposals": `{"repository_id":"nope"}`,
	} {
		status, text := postJSON(t, e.srv.URL+"/api/"+path, body)
		var out map[string]any
		_ = json.Unmarshal([]byte(text), &out)
		want := "PROPOSAL_NOT_FOUND"
		if strings.HasPrefix(path, "list") {
			want = "REPOSITORY_NOT_FOUND"
		}
		if status != http.StatusNotFound || out["code"] != want || out["error"] == nil {
			t.Errorf("%s = %d %s", path, status, text)
		}
	}
}
