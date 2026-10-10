package ports_test

import (
	"encoding/json"
	"testing"

	"operators-mcp/internal/ports"
)

func TestCodeChangedGoesToItsProjectAndKeepsTheProjectOffTheWire(t *testing.T) {
	c := ports.ProjectChange{CodeChanged: &ports.CodeChange{WorkspaceID: "w1", Head: "abc", ProjectID: "p1"}}
	if c.ProjectID() != "p1" {
		t.Errorf("ProjectID = %q, want p1", c.ProjectID())
	}
	b, _ := json.Marshal(c)
	if string(b) != `{"code_changed":{"workspace_id":"w1","head":"abc"}}` {
		t.Errorf("wire = %s", b)
	}
}
