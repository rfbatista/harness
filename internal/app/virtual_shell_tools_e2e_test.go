//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"operators-mcp/internal/domain"
)

// The shell replaces the resource tools (contract "Harness server ↔ Agent
// sessions — virtual shell" §7, §8).

// removedTaskTools are the tools the contract's §7 says are no longer served.
var removedTaskTools = []string{
	"list_task_documents", "read_task_document", "create_task_document", "update_task_document", "edit_task_document",
	"list_project_documents", "read_project_document", "update_project_document", "edit_project_document",
	"move_document_to_project", "move_document_to_task", "search_documents",
	"add_task_comment", "update_task_comment", "delete_task_comment", "list_task_comments",
	"list_task_resources", "read_task_resource",
	"list_task_artifacts", "list_project_artifacts", "read_artifact", "unpublish_artifact",
	"move_artifact_to_project", "move_artifact_to_task",
}

// A task session is served none of the removed tools; get_task answers the
// task without its documents; publish_artifact takes a dev server's
// loopback URL and refuses a file, pointing at task/designs/.
func TestTheResourceToolsAreGoneAndPublishTakesOnlyURLs(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell tools")
	doc := e.createDocument("Spec", "# Spec", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	a := e.start(taskA)
	mcp := e.dial(a)

	listed, err := mcp.ListTools(context.Background(), mcplib.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	served := map[string]bool{}
	for _, tool := range listed.Tools {
		served[tool.Name] = true
	}
	for _, name := range removedTaskTools {
		if served[name] {
			t.Errorf("%s is still served", name)
		}
	}
	for _, name := range []string{"get_task", "publish_artifact", "attach_artifact_to_task", "detach_artifact_from_task", "update_task_status"} {
		if !served[name] {
			t.Errorf("%s is no longer served", name)
		}
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(callTask(t, mcp, "get_task", nil)), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["documents"]; ok {
		t.Errorf("get_task still lists documents: %s", got["documents"])
	}
	if !strings.Contains(string(got["task"]), taskA) {
		t.Errorf("get_task task = %s, want task %s", got["task"], taskA)
	}

	if err := os.WriteFile(filepath.Join(a.worktree, "design.html"), []byte("<h1>v1</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	refused := callTaskErr(t, mcp, "publish_artifact", map[string]any{"path": "design.html", "title": "Design"})
	if !strings.HasPrefix(refused, "PUBLISH_URL_ONLY: ") || !strings.Contains(refused, "task/designs/") {
		t.Errorf("publishing a file = %q, want PUBLISH_URL_ONLY pointing at task/designs/", refused)
	}
	var pub struct {
		ArtifactID string              `json:"artifact_id"`
		Kind       domain.ArtifactKind `json:"kind"`
	}
	if err := json.Unmarshal([]byte(callTask(t, mcp, "publish_artifact", map[string]any{"url": "http://127.0.0.1:5173/", "title": "Dev server"})), &pub); err != nil {
		t.Fatal(err)
	}
	if pub.ArtifactID == "" || pub.Kind != domain.ArtifactURL {
		t.Fatalf("publishing a loopback url = %+v", pub)
	}
	eventually(t, "the url in task/designs/urls.md", func() error {
		return fileWith(a.shell, "task/designs/urls.md", "[Dev server](http://127.0.0.1:5173/)")
	})
}

// A comment or resource the person adds while a session runs reaches it as
// a [task update · …] turn naming the file's path in its shell, the path
// the shell actually holds it at, even when the name clashed.
func TestUpdateTurnsNameTheShellPath(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell turns")
	first := e.upload(taskA, "mockup.txt", "the first mockup")
	a := e.start(taskA)

	r := e.upload(taskA, "mockup.txt", "the second mockup")
	turn := nextTurn(t, e.orch, a.id)
	rel := indexedPath(t, a.shell, r.ID)
	if err := hasFile(a.shell, "task/resources/mockup.txt", "the first mockup", first.ID, "task", ""); err != nil {
		t.Fatalf("the first mockup.txt lost its name: %v", err)
	}
	if err := hasFile(a.shell, rel, "the second mockup", r.ID, "task", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn, "[task update · resource added] ") || !strings.Contains(turn, rel) || strings.Contains(turn, "read_task_resource") {
		t.Fatalf("resource turn = %q, want it to name %s", turn, rel)
	}

	e.post("add_ticket_link", map[string]string{"ticket_id": taskA, "url": "https://example.com/spec", "title": "Spec page"}, nil)
	if turn := nextTurn(t, e.orch, a.id); !strings.Contains(turn, "task/resources/links.md") || !strings.Contains(turn, "https://example.com/spec") {
		t.Fatalf("link turn = %q, want it to name task/resources/links.md", turn)
	}

	var added struct {
		Comment domain.TicketComment `json:"comment"`
	}
	e.post("add_ticket_comment", map[string]string{"ticket_id": taskA, "body": "Please keep names stable."}, &added)
	turn = nextTurn(t, e.orch, a.id)
	rel = indexedPath(t, a.shell, added.Comment.ID)
	if !strings.HasPrefix(turn, "[task update · comment] ") || !strings.Contains(turn, rel) || !strings.Contains(turn, "Please keep names stable.") ||
		strings.Contains(turn, "list_task_comments") {
		t.Fatalf("comment turn = %q, want it to name %s", turn, rel)
	}
}

// indexedPath is where the shell's index holds the file with id.
func indexedPath(t *testing.T, dir, id string) string {
	t.Helper()
	var rel string
	eventually(t, "the file "+id+" in the shell", func() error {
		idx, err := shellIndex(dir)
		if err != nil {
			return err
		}
		for p, row := range idx {
			if row.ID == id {
				rel = p
				return nil
			}
		}
		return fmt.Errorf("%s is not indexed", id)
	})
	return rel
}
