package ports

import (
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// A resource or comment travels alone under its key, whole; once deleted
// only its ids go, with deleted set. Both route to their project.
func TestProjectChange_TicketResourceAndComment(t *testing.T) {
	r := &domain.TicketResource{ID: "r1", TicketID: "t1", ProjectID: "p1", Kind: domain.ResourceImage, Title: "Shot", Mime: "image/png", SizeBytes: 3, ContentURL: "/c"}
	cm := &domain.TicketComment{ID: "c1", TicketID: "t1", ProjectID: "p1", Author: domain.CommentByPerson, Body: "hi"}
	cases := []struct {
		name   string
		change ProjectChange
		keys   string
		body   string
	}{
		{"resource", TicketResourceUpdated(r), "ticket_resource", `"title":"Shot"`},
		{"resource removed", TicketResourceRemoved(r), "deleted,ticket_resource", `"ticket_resource":{"id":"r1","ticket_id":"t1","project_id":"p1"}`},
		{"comment", TicketCommentUpdated(cm), "ticket_comment", `"body":"hi"`},
		{"comment removed", TicketCommentRemoved(cm), "deleted,ticket_comment", `"ticket_comment":{"id":"c1","ticket_id":"t1","project_id":"p1"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.change.ProjectID() != "p1" {
				t.Fatalf("ProjectID = %q", c.change.ProjectID())
			}
			b, err := json.Marshal(c.change)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(wireKeys(t, b), ","); got != c.keys {
				t.Fatalf("keys = %s, want %s: %s", got, c.keys, b)
			}
			if !strings.Contains(string(b), c.body) {
				t.Fatalf("wire %s lacks %s", b, c.body)
			}
			var back ProjectChange
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			if back.ProjectID() != "p1" {
				t.Fatalf("decoded ProjectID = %q from %s", back.ProjectID(), b)
			}
		})
	}
}
