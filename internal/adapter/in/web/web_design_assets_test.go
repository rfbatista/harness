package web

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeArtifacts lists like the artifacts service: by task, what it produced
// and what is attached to it; by project, narrowed by scope.
type fakeArtifacts struct{ list []*domain.Artifact }

func (f fakeArtifacts) ListArtifacts(_ context.Context, flt ports.ArtifactFilter) ([]*domain.Artifact, error) {
	out := []*domain.Artifact{}
	for _, a := range f.list {
		if flt.TicketID != "" && a.TicketID != flt.TicketID && !a.AttachedTo(flt.TicketID) {
			continue
		}
		if flt.ProjectID != "" && a.ProjectID != flt.ProjectID {
			continue
		}
		if flt.Scope != "" && a.Scope != flt.Scope {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (fakeArtifacts) GetArtifact(context.Context, string) (*domain.Artifact, error) { return nil, nil }
func (fakeArtifacts) DeleteArtifact(context.Context, string) error                  { return nil }
func (fakeArtifacts) OpenArtifactFile(context.Context, string, string) (*ports.ArtifactFile, error) {
	return nil, nil
}

// designBoard is the board with a logo t-ship produced and moved to the
// project, attached to t-feed; a draft t-feed produced; and p2's own asset.
func designBoard() world {
	w := board()
	w.artifacts = []*domain.Artifact{
		{ID: "a-logo", TicketID: "t-ship", ProjectID: "p1", Kind: domain.ArtifactPage, Title: "Logo", Path: "logo.html",
			Revision: 2, Scope: domain.ArtifactScopeProject, AttachedTicketIDs: []string{"t-feed"}},
		{ID: "a-draft", TicketID: "t-feed", ProjectID: "p1", Kind: domain.ArtifactImage, Title: "Draft", Path: "draft.png",
			Revision: 1, Scope: domain.ArtifactScopeTask},
		{ID: "a-else", TicketID: "t-else", ProjectID: "p2", Kind: domain.ArtifactPage, Title: "Else", Path: "else.html",
			Revision: 1, Scope: domain.ArtifactScopeProject},
	}
	return w
}

// seedOf reads the JSON a page embeds under script id.
func seedOf(t *testing.T, body, id string) map[string]any {
	t.Helper()
	m := regexp.MustCompile(`(?s)<script id="` + id + `" type="application/json">(.*?)</script>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %s in:\n%s", id, body)
	}
	var seed map[string]any
	if err := json.Unmarshal([]byte(m[1]), &seed); err != nil {
		t.Fatalf("%s: %v\n%s", id, err, m[1])
	}
	return seed
}

func artifactRows(t *testing.T, seed map[string]any) map[string]string {
	t.Helper()
	raw, ok := seed["artifacts"].([]any)
	if !ok {
		t.Fatalf("artifacts = %#v, want a list", seed["artifacts"])
	}
	rows := map[string]string{}
	for _, r := range raw {
		a := r.(map[string]any)
		ids, ok := a["attached_ticket_ids"].([]any)
		if !ok {
			t.Fatalf("%v: attached_ticket_ids = %#v", a["id"], a["attached_ticket_ids"])
		}
		parts := []string{a["ticket_id"].(string)}
		for _, id := range ids {
			parts = append(parts, id.(string))
		}
		rows[a["id"].(string)] = strings.Join(parts, ">")
	}
	return rows
}

// The task page seeds the task's design assets (produced and attached) and
// the project's tasks with their pages.
func TestTaskPageSeedsItsDesignAssetsAndTheProjectTasks(t *testing.T) {
	rec := get(t, newTestHandler(t, designBoard()), "/projects/p1/tasks/t-feed")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	seed := seedOf(t, rec.Body.String(), "sessions-seed")
	rows := artifactRows(t, seed)
	if len(rows) != 2 || rows["a-logo"] != "t-ship>t-feed" || rows["a-draft"] != "t-feed" {
		t.Errorf("artifacts = %v, want the draft it made and the logo attached to it", rows)
	}
	tasks, _ := seed["tasks"].([]any)
	links := map[string]string{}
	for _, raw := range tasks {
		tk := raw.(map[string]any)
		links[tk["id"].(string)] = tk["title"].(string) + " " + tk["href"].(string)
	}
	if len(links) != 3 || links["t-ship"] != "Ship it /projects/p1/tasks/t-ship" {
		t.Errorf("tasks = %v, want p1's three with their pages", links)
	}
}

// Without an artifact reader the seed still says [] rather than null.
func TestTaskPageSeedsNoArtifactsAsAnEmptyList(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed")
	body := rec.Body.String()
	if !strings.Contains(body, `"artifacts":[]`) || !strings.Contains(body, `"tasks":[{`) {
		t.Fatalf("seed lacks artifacts [] or tasks:\n%s", seedOf(t, body, "sessions-seed"))
	}
}

// The design library seeds the project's project assets with the tasks each
// is attached to.
func TestProjectDesignPageSeedsTheProjectAssets(t *testing.T) {
	rec := get(t, newTestHandler(t, designBoard()), "/projects/p1/design")
	seed := designSeed(t, rec.Body.String())
	if rows := artifactRows(t, seed); len(rows) != 1 || rows["a-logo"] != "t-ship>t-feed" {
		t.Errorf("artifacts = %v, want only p1's logo", rows)
	}
	rec = get(t, newTestHandler(t, board()), "/projects/p1/design")
	if !strings.Contains(rec.Body.String(), `"artifacts":[]`) {
		t.Errorf("no reader: want artifacts []")
	}
}
