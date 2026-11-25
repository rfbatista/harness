//go:build !windows && sqlite_fts5

package app

import (
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

const searchTool = "search_past_conversations"

func TestTheProductOwnerSearchesItsProjectsPastConversationsAndNeverAnotherProjects(t *testing.T) {
	e := newPOE2E(t)
	old := e.checkpointedThread("we decided to export the tasks as csv", "and then webhooks")
	title := e.thread(old).Thread.Title

	other, err := e.cat.Projects.CreateProject(e.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		ThreadID string `json:"thread_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": other.ID, "message": "zebra crossing plans"}, &started); status != 200 {
		t.Fatalf("start = %d %s", status, code)
	}
	e.waitTurn(started.ThreadID, "idle")

	// A new thread of the project, as the product owner would be asked in.
	asking, _ := e.startThread("what did we decide about the export?")
	e.waitTurn(asking, "idle")
	owner := e.dialSession(e.thread(asking).Thread.SessionID)

	// Words of an archived part come back with the thread, its date and where
	// the passage lies.
	text, refused := e.call(owner, searchTool, map[string]any{"query": "exports"})
	if refused {
		t.Fatalf("search refused: %s", text)
	}
	for _, want := range []string{title, old, `"archived":true`, `"at":"`, "«export»"} {
		if !strings.Contains(text, want) {
			t.Errorf("answer lacks %q: %s", want, text)
		}
	}

	// Another project's conversation is out of reach, and an empty answer says
	// that it searched.
	text, refused = e.call(owner, searchTool, map[string]any{"query": "zebra"})
	if refused || !strings.Contains(text, "nothing found") || strings.Contains(text, started.ThreadID) {
		t.Errorf("zebra: refused %v, answer %s, want a plain nothing found", refused, text)
	}
	if text, refused = e.call(owner, searchTool, map[string]any{"query": "  "}); !refused {
		t.Errorf("an empty query was answered: %s", text)
	}

	// What the product owner is told about the tool.
	listed, err := owner.ListTools(e.ctx, mcplib.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var description string
	for _, tool := range listed.Tools {
		if tool.Name == searchTool {
			description = tool.Description
		}
	}
	for _, want := range []string{"synonyms", "phrasings", "OR"} {
		if !strings.Contains(description, want) {
			t.Errorf("the tool's description lacks %q: %s", want, description)
		}
	}
	launches := e.launches()
	if !strings.Contains(launches[len(launches)-1], "Search before you say you do not remember") ||
		!strings.Contains(launches[len(launches)-1], "name the thread and the date") {
		t.Errorf("the brief does not tell it to search and cite: %.300s", launches[len(launches)-1])
	}
}
