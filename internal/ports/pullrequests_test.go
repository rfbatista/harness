package ports

import (
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func TestProjectChange_PullRequest(t *testing.T) {
	c := ProjectChange{PullRequest: &domain.PullRequest{WorkspaceID: "w1", ProjectID: "p1", Number: 4, State: domain.PullRequestMerged}}
	if c.ProjectID() != "p1" {
		t.Fatalf("ProjectID = %q", c.ProjectID())
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(wireKeys(t, b), ","); got != "pull_request" {
		t.Fatalf("keys = %s, want only pull_request: %s", got, b)
	}
	if strings.Contains(string(b), "p1") {
		t.Fatalf("the project id leaked onto the wire: %s", b)
	}
}
