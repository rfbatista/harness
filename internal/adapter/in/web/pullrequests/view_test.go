package pullrequests

import (
	"encoding/json"
	"os"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The pages paint what the browser's view.js says: both read
// web/testdata/views/pull-request.json.
func TestViewsMatchTheBrowser(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/pull-request.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Draft    domain.PullRequestDraft `json:"draft"`
		Blockers []struct {
			Code, Text, Href string
			Remote           *domain.GitRemote
		}
		Chips    []struct {
			State string
			Draft bool
			Shown string
			Tone  *string
		}
		Summaries []struct {
			Ahead  int
			Remote *domain.GitRemote
			Text   string
		}
		Uncommitted []struct {
			Count int
			Text  string
		}
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Blockers {
		d := v.Draft
		d.Remote = c.Remote
		if got := BlockerText(c.Code, d); got != c.Text {
			t.Errorf("%s: %q, want %q", c.Code, got, c.Text)
		}
		if href, _ := BlockerLink(c.Code, d); href != c.Href {
			t.Errorf("%s: link %q, want %q", c.Code, href, c.Href)
		}
	}
	for _, c := range v.Chips {
		chip := NewChip(&domain.PullRequest{Number: 4, State: domain.PullRequestState(c.State), Draft: c.Draft})
		tone := ""
		if c.Tone != nil {
			tone = *c.Tone
		}
		if chip.State != c.Shown || chip.Tone != tone {
			t.Errorf("%s draft=%v: %s/%q, want %s/%q", c.State, c.Draft, chip.State, chip.Tone, c.Shown, tone)
		}
	}
	for _, c := range v.Summaries {
		d := v.Draft
		d.Ahead, d.Remote = c.Ahead, c.Remote
		if got := Summary(d); got != c.Text {
			t.Errorf("summary %q, want %q", got, c.Text)
		}
	}
	for _, c := range v.Uncommitted {
		d := v.Draft
		d.UncommittedChanges = c.Count
		if got := Uncommitted(d); got != c.Text {
			t.Errorf("uncommitted %q, want %q", got, c.Text)
		}
	}
}

func TestPanelPaintsWhatTheViewSays(t *testing.T) {
	open := &domain.PullRequest{Number: 7, State: "open", Title: "T", URL: "https://github.com/a/b/pull/7"}
	merged := &domain.PullRequest{Number: 6, State: "merged"}
	blocked := domain.PullRequestDraft{HeadBranch: "agent/x", BaseBranch: "main", Blockers: []string{"PR_NO_COMMITS"}}

	if p := NewPanel("p1", "ws1", nil); p.Painted() || p.OffersCreate || p.Chip != nil {
		t.Errorf("no view paints nothing: %+v", p)
	}
	if p := NewPanel("p1", "ws1", &ports.PullRequestView{Draft: blocked}); !p.OffersCreate || !p.Blocked() || p.Blockers[0] != "Nothing to propose yet: agent/x has no commits ahead of main." {
		t.Errorf("blocked: %+v", p)
	}
	if p := NewPanel("p1", "ws1", &ports.PullRequestView{Draft: blocked, PullRequest: open}); p.OffersCreate || p.Blocked() || p.Chip.Label != "#7" || p.Chip.Tone != "success" {
		t.Errorf("an open PR shows, no create, no reasons: %+v", p)
	}
	if p := NewPanel("p1", "ws1", &ports.PullRequestView{Draft: domain.PullRequestDraft{}, PullRequest: merged}); !p.OffersCreate || p.Blocked() || p.Chip.State != "merged" {
		t.Errorf("a merged PR beside the create: %+v", p)
	}
}

func TestPanelSeedIsTheContractsShape(t *testing.T) {
	b, _ := json.Marshal(NewPanel("p1", "ws1", nil).Seed)
	if string(b) != `{"project_id":"p1","workspace_id":"ws1"}` {
		t.Errorf("without a view: %s", b)
	}
	b, _ = json.Marshal(NewPanel("p1", "ws1", &ports.PullRequestView{Draft: domain.PullRequestDraft{WorkspaceID: "ws1", Blockers: []string{}}}).Seed)
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if _, ok := got["draft"]; !ok || got["pull_request"] != nil {
		t.Errorf("with a view: %s", b)
	}
}
