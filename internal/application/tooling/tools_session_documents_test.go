package tooling

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// doc calls a tool that answers {document} as the fixture's session and
// returns the document; a failure ends the test.
func (f *taskToolsFixture) doc(t *testing.T, tool string, args map[string]any) *domain.Document {
	t.Helper()
	out, err := f.call(t, f.sessionID, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)["document"].(*domain.Document)
}

// summaries calls a tool that answers {documents} as the fixture's session.
func (f *taskToolsFixture) summaries(t *testing.T, tool string, args map[string]any) []documentSummary {
	t.Helper()
	out, err := f.call(t, f.sessionID, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)["documents"].([]documentSummary)
}

func hasID(list []documentSummary, id string) bool {
	for _, d := range list {
		if d.ID == id {
			return true
		}
	}
	return false
}

// What one task's session moves to the project, every session of the
// project can list; the task that wrote it keeps it.
func TestSessionDocumentTools_MoveToProjectAndBack(t *testing.T) {
	f := newTaskToolsFixture(t)
	created := f.doc(t, "create_task_document", map[string]any{"title": "Contract", "content": htmlPage("Contract", "x")})
	if created.Scope != domain.DocumentScopeTask {
		t.Fatalf("a session's new document is scope %q, want task", created.Scope)
	}
	if list := f.summaries(t, "list_project_documents", nil); len(list) != 0 {
		t.Fatalf("a task document is listed as a project document: %+v", list)
	}
	time.Sleep(2 * time.Millisecond)
	moved := f.doc(t, "move_document_to_project", map[string]any{"document_id": created.ID})
	if moved.ID != created.ID || moved.Scope != domain.DocumentScopeProject || !moved.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("move: %+v", moved)
	}
	if list := f.summaries(t, "list_project_documents", nil); len(list) != 1 || list[0].Scope != domain.DocumentScopeProject {
		t.Fatalf("project listing: %+v", list)
	}
	if list := f.summaries(t, "list_task_documents", nil); !hasID(list, created.ID) {
		t.Fatalf("the task lost its moved document: %+v", list)
	}
	again := f.doc(t, "move_document_to_project", map[string]any{"document_id": created.ID})
	if !again.UpdatedAt.Equal(moved.UpdatedAt) {
		t.Fatalf("idempotent move wrote: %v then %v", moved.UpdatedAt, again.UpdatedAt)
	}
	back := f.doc(t, "move_document_to_task", map[string]any{"document_id": created.ID})
	if back.Scope != domain.DocumentScopeTask {
		t.Fatalf("move back: %+v", back)
	}
	if list := f.summaries(t, "list_project_documents", nil); len(list) != 0 {
		t.Fatalf("still a project document: %+v", list)
	}
}

// A session moves only what is linked to its own task: not another task's
// documents, not a project document linked to no task, nothing by a missing id.
func TestSessionDocumentTools_MoveNeedsOwnTaskLink(t *testing.T) {
	f := newTaskToolsFixture(t)
	other, _ := f.plan.CreateTicket(context.Background(), f.projectID, "Elsewhere", "", domain.TicketStatusTodo)
	theirs, _ := f.plan.CreateDocument(f.projectID, "Theirs", "# t", "", domain.DocumentScopeTask)
	_ = f.plan.LinkDocument(other.ID, theirs.ID)
	loose, _ := f.plan.CreateDocument(f.projectID, "Conventions", "# c", "", domain.DocumentScopeProject)

	for _, name := range []string{"move_document_to_project", "move_document_to_task"} {
		_, err := f.call(t, f.sessionID, name, map[string]any{"document_id": theirs.ID})
		wantCode(t, err, "DOCUMENT_NOT_ON_TASK")
		_, err = f.call(t, f.sessionID, name, map[string]any{"document_id": loose.ID})
		wantCode(t, err, "DOCUMENT_NOT_ON_TASK")
		_, err = f.call(t, f.sessionID, name, map[string]any{"document_id": "missing"})
		wantCode(t, err, "DOCUMENT_NOT_ON_TASK")
		_, err = f.call(t, f.sessionID, name, map[string]any{})
		wantCode(t, err, "INVALID_INPUT")
		_, err = f.call(t, "", name, map[string]any{"document_id": theirs.ID})
		wantCode(t, err, "SESSION_NOT_FOUND")
	}
	if got := f.plan.GetDocument(theirs.ID); got.Scope != domain.DocumentScopeTask {
		t.Fatalf("a refused move changed the document: %+v", got)
	}
}

// Any project document of the project is readable and revisable, from any
// task, with the task tools' partial-edit and HTML-only rules.
func TestSessionDocumentTools_ReadAndReviseAnyProjectDocument(t *testing.T) {
	f := newTaskToolsFixture(t)
	arch, err := f.plan.CreateDocument(f.projectID, "Architecture", "# arch", domain.DocumentFormatMarkdown, domain.DocumentScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if list := f.summaries(t, "list_project_documents", nil); !hasID(list, arch.ID) {
		t.Fatalf("an unlinked project document is not listed: %+v", list)
	}
	read := f.doc(t, "read_project_document", map[string]any{"document_id": arch.ID})
	if read.Content != "# arch" || read.Format != domain.DocumentFormatMarkdown || read.Scope != domain.DocumentScopeProject {
		t.Fatalf("read back %+v", read)
	}
	retitled := f.doc(t, "update_project_document", map[string]any{"document_id": arch.ID, "title": "Architecture v2"})
	if retitled.Title != "Architecture v2" || retitled.Content != "# arch" || retitled.Format != domain.DocumentFormatMarkdown {
		t.Fatalf("title-only update touched more than the title: %+v", retitled)
	}
	_, err = f.call(t, f.sessionID, "update_project_document", map[string]any{"document_id": arch.ID, "content": "# new"})
	wantCode(t, err, "DOCUMENT_NOT_HTML")
	if got := f.plan.GetDocument(arch.ID); got.Content != "# arch" {
		t.Fatalf("a refused update changed the document: %+v", got)
	}
	revised := f.doc(t, "update_project_document", map[string]any{"document_id": arch.ID, "content": htmlPage("Architecture", "new")})
	if revised.Format != domain.DocumentFormatHTML || revised.Content != htmlPage("Architecture", "new") || revised.Title != "Architecture v2" || revised.Scope != domain.DocumentScopeProject {
		t.Fatalf("content update: %+v", revised)
	}
}

// read_project_document and update_project_document reach project documents
// of this project only: not a task document (even one of this task), not
// another project's document, nothing missing.
func TestSessionDocumentTools_ReachIsProjectScoped(t *testing.T) {
	f := newTaskToolsFixture(t)
	mine := f.doc(t, "create_task_document", map[string]any{"title": "Plan", "content": htmlPage("Plan", "x")})
	other, _ := f.plan.CreateTicket(context.Background(), f.projectID, "Elsewhere", "", domain.TicketStatusTodo)
	theirs, _ := f.plan.CreateDocument(f.projectID, "Theirs", "# t", "", domain.DocumentScopeTask)
	_ = f.plan.LinkDocument(other.ID, theirs.ID)
	p2, err := f.projects.Create("p2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := f.plan.CreateDocument(p2.ID, "Foreign", "# f", "", domain.DocumentScopeProject)

	for _, name := range []string{"read_project_document", "update_project_document"} {
		for _, id := range []string{mine.ID, theirs.ID, foreign.ID, "missing"} {
			_, err := f.call(t, f.sessionID, name, map[string]any{"document_id": id, "title": "x"})
			wantCode(t, err, "DOCUMENT_NOT_IN_PROJECT")
		}
		_, err := f.call(t, f.sessionID, name, map[string]any{})
		wantCode(t, err, "INVALID_INPUT")
	}
	if got := f.plan.GetDocument(foreign.ID); got.Title != "Foreign" {
		t.Fatalf("a refused update changed another project's document: %+v", got)
	}
}

func TestSessionDocumentTools_SummariesCarryScope(t *testing.T) {
	f := newTaskToolsFixture(t)
	created := f.doc(t, "create_task_document", map[string]any{"title": "Plan", "content": htmlPage("Plan", "x")})
	list := f.summaries(t, "list_task_documents", nil)
	b, _ := json.Marshal(list[0])
	if list[0].ID != created.ID || !strings.Contains(string(b), `"scope":"task"`) {
		t.Fatalf("summary lacks the scope: %s", b)
	}
}
