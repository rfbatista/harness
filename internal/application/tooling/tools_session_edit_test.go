package tooling

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// An edit changes one row of a page without the agent sending the page, and
// answers the document's new size and version, not its content.
func TestEditTaskDocument_ChangesOnePartAndAnswersTheNewVersion(t *testing.T) {
	f := newTaskToolsFixture(t)
	d := f.taskDoc(t, "Plan", htmlPage("Plan", "<h1>Plan</h1><p>step one</p><p>step two</p>"))
	got := f.read(t, "edit_task_document", map[string]any{"document_id": d.ID, "old_string": "<p>step two</p>",
		"new_string": "<p>step two</p><p>step three</p>", "expected_version": domain.DocumentVersion(d.Content)})
	doc := got["document"].(map[string]any)
	stored := f.plan.GetDocument(d.ID)
	if _, ok := doc["content"]; ok || doc["id"] != d.ID || doc["lines"] != float64(4) || doc["bytes"] != float64(len(stored.Content)) ||
		doc["version"] != domain.DocumentVersion(stored.Content) || !strings.Contains(stored.Content, "<p>step three</p>") {
		t.Fatalf("edit answered %v; stored %q", doc, stored.Content)
	}

	_, err := f.call(t, f.sessionID, "edit_task_document", map[string]any{"document_id": d.ID, "old_string": "<p>step one</p>",
		"new_string": "x", "expected_version": domain.DocumentVersion(d.Content)})
	wantCode(t, err, "DOCUMENT_CHANGED")
	if !strings.HasPrefix(err.(*domain.StructuredError).Message, "DOCUMENT_CHANGED: now at "+domain.DocumentVersion(stored.Content)) {
		t.Fatalf("stale version: %v", err)
	}
	for code, args := range map[string]map[string]any{
		"EDIT_NO_MATCH":     {"old_string": "<p>step four</p>", "new_string": "x"},
		"DOCUMENT_NOT_HTML": {"old_string": "<body>", "new_string": "<main>"},
		"INVALID_INPUT":     {"old_string": "<p>step one</p>"},
	} {
		args["document_id"] = d.ID
		_, err := f.call(t, f.sessionID, "edit_task_document", args)
		wantCode(t, err, code)
		if !strings.HasPrefix(err.(*domain.StructuredError).Message, code+": ") {
			t.Fatalf("not coded: %v", err)
		}
	}
	if f.plan.GetDocument(d.ID).Content != stored.Content {
		t.Fatal("a refused edit changed the document")
	}
}

// The edit tools reach what the update tools reach, nothing more.
func TestEditDocuments_Scope(t *testing.T) {
	f := newTaskToolsFixture(t)
	mine := f.taskDoc(t, "Plan", htmlPage("Plan", "<p>a</p>"))
	other, _ := f.plan.CreateTicket(context.Background(), f.projectID, "Elsewhere", "", domain.TicketStatusTodo)
	theirs, _ := f.plan.CreateDocument(f.projectID, "Theirs", htmlPage("T", "<p>a</p>"), domain.DocumentFormatHTML, domain.DocumentScopeTask)
	_ = f.plan.LinkDocument(other.ID, theirs.ID)
	arch, _ := f.plan.CreateDocument(f.projectID, "Architecture", htmlPage("A", "<p>a</p>"), domain.DocumentFormatHTML, domain.DocumentScopeProject)
	notes, _ := f.plan.CreateDocument(f.projectID, "Notes", "# a", domain.DocumentFormatMarkdown, domain.DocumentScopeProject)

	edit := func(tool, id string) error {
		_, err := f.call(t, f.sessionID, tool, map[string]any{"document_id": id, "old_string": "<p>a</p>", "new_string": "<p>b</p>"})
		return err
	}
	wantCode(t, edit("edit_task_document", theirs.ID), "DOCUMENT_NOT_ON_TASK")
	wantCode(t, edit("edit_task_document", arch.ID), "DOCUMENT_NOT_ON_TASK")
	wantCode(t, edit("edit_project_document", mine.ID), "DOCUMENT_NOT_IN_PROJECT")
	if err := edit("edit_project_document", arch.ID); err != nil {
		t.Fatal(err)
	}
	err := edit("edit_project_document", notes.ID)
	wantCode(t, err, "DOCUMENT_NOT_HTML")
	if !strings.Contains(err.Error(), "this document is Markdown; rewrite it once as HTML with update_*_document, then edit") {
		t.Fatalf("markdown: %v", err)
	}
	if got := f.plan.GetDocument(theirs.ID); strings.Contains(got.Content, "<p>b</p>") {
		t.Fatal("another task's document was edited")
	}
}
