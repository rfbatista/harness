package ports

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func wireKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not an object: %s", raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// A changed artifact travels alone under "artifact", whole, and routes to
// its project.
func TestProjectChange_ArtifactUpdated(t *testing.T) {
	a := &domain.Artifact{ID: "a1", TicketID: "t1", ProjectID: "p1", Title: "Logo", Scope: domain.ArtifactScopeProject, AttachedTicketIDs: []string{"t2"}}
	c := ArtifactUpdated(a)
	if c.ProjectID() != "p1" {
		t.Fatalf("ProjectID = %q", c.ProjectID())
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(wireKeys(t, b), ","); got != "artifact" {
		t.Fatalf("keys = %s, want only artifact: %s", got, b)
	}
	var back ProjectChange
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Artifact == nil || back.Artifact.Artifact.Title != "Logo" || !back.Artifact.Artifact.AttachedTo("t2") || back.Deleted {
		t.Fatalf("round trip = %+v", back.Artifact)
	}
}

// A deleted artifact carries only its ids as they were, beside deleted: true.
func TestProjectChange_ArtifactRemoved(t *testing.T) {
	a := &domain.Artifact{ID: "a1", TicketID: "t1", ProjectID: "p1", Title: "Logo", Kind: domain.ArtifactPage, Revision: 3,
		Scope: domain.ArtifactScopeProject, AttachedTicketIDs: []string{"t2"}}
	c := ArtifactRemoved(a)
	if !c.Deleted || c.ProjectID() != "p1" {
		t.Fatalf("change = %+v, project %q", c, c.ProjectID())
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(wireKeys(t, b), ","); got != "artifact,deleted" {
		t.Fatalf("keys = %s: %s", got, b)
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(b, &top)
	if got := strings.Join(wireKeys(t, top["artifact"]), ","); got != "attached_ticket_ids,id,project_id,ticket_id" {
		t.Fatalf("deleted artifact keys = %s: %s", got, top["artifact"])
	}
	var back ProjectChange
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Deleted || back.Artifact == nil || back.Artifact.Artifact.ID != "a1" || back.ProjectID() != "p1" || !back.Artifact.Artifact.AttachedTo("t2") {
		t.Fatalf("round trip = %+v %+v", back, back.Artifact)
	}
}

// A deleted artifact that had no attachments still says so with [].
func TestProjectChange_ArtifactRemovedNoAttachments(t *testing.T) {
	b, _ := json.Marshal(ArtifactRemoved(&domain.Artifact{ID: "a1", TicketID: "t1", ProjectID: "p1"}))
	if !strings.Contains(string(b), `"attached_ticket_ids":[]`) {
		t.Fatalf("want attached_ticket_ids []: %s", b)
	}
}
