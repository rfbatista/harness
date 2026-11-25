//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type attributedBy struct {
	Kind      string `json:"kind"`
	SessionID string `json:"session_id"`
	ThreadID  string `json:"thread_id"`
	At        string `json:"at"`
}

type attributedRow struct {
	ID        string        `json:"id"`
	CreatedBy *attributedBy `json:"created_by"`
	ChangedBy *attributedBy `json:"changed_by"`
}

func (e *poE2E) taskRow(id string) attributedRow {
	e.t.Helper()
	var out struct {
		Ticket attributedRow `json:"ticket"`
	}
	if status, code := e.get("get_ticket?ticket_id="+id, &out); status != 200 {
		e.t.Fatalf("GET get_ticket %s = %d %s", id, status, code)
	}
	return out.Ticket
}

func (e *poE2E) documentRow(id string) attributedRow {
	e.t.Helper()
	var out struct {
		Documents []attributedRow `json:"documents"`
	}
	if status, code := e.get("list_documents?project_id="+e.project, &out); status != 200 {
		e.t.Fatalf("GET list_documents = %d %s", status, code)
	}
	for _, d := range out.Documents {
		if d.ID == id {
			return d
		}
	}
	e.t.Fatalf("document %s is not listed", id)
	return attributedRow{}
}

func TestATaskAndADocumentRecordWhoMadeAndLastChangedThem(t *testing.T) {
	e := newPOE2E(t)

	// The person, through the web UI's API.
	var made struct {
		Ticket attributedRow `json:"ticket"`
	}
	if status, code := e.post("create_ticket", map[string]any{"project_id": e.project, "title": "By the person"}, &made); status != 200 {
		t.Fatalf("create_ticket = %d %s", status, code)
	}
	row := e.taskRow(made.Ticket.ID)
	if row.CreatedBy == nil || row.CreatedBy.Kind != "person" || row.CreatedBy.At == "" || row.ChangedBy != nil {
		t.Fatalf("a new task by the person: created_by %+v changed_by %+v", row.CreatedBy, row.ChangedBy)
	}
	if status, code := e.post("update_ticket", map[string]any{"ticket_id": made.Ticket.ID, "title": "Renamed"}, nil); status != 200 {
		t.Fatalf("update_ticket = %d %s", status, code)
	}
	if row = e.taskRow(made.Ticket.ID); row.ChangedBy == nil || row.ChangedBy.Kind != "person" || row.ChangedBy.At == "" {
		t.Fatalf("after a change by the person: changed_by %+v", row.ChangedBy)
	}

	// A session, through its tools.
	if _, err := e.sess.Create(&domain.Session{ID: "task-session", ProjectID: e.project, Task: "a session", Status: domain.SessionRunning, Mode: domain.SessionModeProduct}); err != nil {
		t.Fatal(err)
	}
	c := e.dialSession("task-session")
	text, refused := e.call(c, "create_task", map[string]any{"title": "By a session", "parent_id": ""})
	if refused {
		t.Fatalf("create_task: %s", text)
	}
	var byTool struct {
		Task attributedRow `json:"task"`
	}
	if err := json.Unmarshal([]byte(text), &byTool); err != nil {
		t.Fatalf("create_task answered %s", text)
	}
	row = e.taskRow(byTool.Task.ID)
	if row.CreatedBy == nil || row.CreatedBy.Kind != "session" || row.CreatedBy.SessionID != "task-session" {
		t.Fatalf("a task made by a session's tool: created_by %+v", row.CreatedBy)
	}
	if text, refused = e.call(c, "update_task_status", map[string]any{"task_id": byTool.Task.ID, "status": "todo"}); refused {
		t.Fatalf("update_task_status: %s", text)
	}
	if row = e.taskRow(byTool.Task.ID); row.ChangedBy == nil || row.ChangedBy.Kind != "session" || row.ChangedBy.SessionID != "task-session" {
		t.Fatalf("a status change by a session: changed_by %+v", row.ChangedBy)
	}

	// A document made through the API is the person's; one a session writes
	// through its virtual shell is the session's.
	var doc struct {
		Document attributedRow `json:"document"`
	}
	if status, code := e.post("create_document", map[string]any{"project_id": e.project, "title": "Notes", "content": "x"}, &doc); status != 200 {
		t.Fatalf("create_document = %d %s", status, code)
	}
	if d := e.documentRow(doc.Document.ID); d.CreatedBy == nil || d.CreatedBy.Kind != "person" || d.ChangedBy != nil {
		t.Fatalf("a document by the person: %+v %+v", d.CreatedBy, d.ChangedBy)
	}
	dir, err := e.shells.Open(e.ctx, "task-session", ports.ShellScope{ProjectID: e.project, TicketID: made.Ticket.ID})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "task/docs/by-session.md")
	if err := os.WriteFile(path, []byte("# By a session\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edit := ports.ShellEdit{Op: "write", Paths: []string{path}, Tool: "Write"}
	if ans := e.shells.CommitShellEdit(e.ctx, "task-session", edit); !ans.Allow {
		t.Fatalf("the shell write = %+v", ans)
	}
	var found bool
	for _, d := range e.plan.ListDocuments(e.project, "") {
		if d.Title == "By a session" {
			found = true
			if d.CreatedBy == nil || d.CreatedBy.Kind != domain.AuthorSession || d.CreatedBy.SessionID != "task-session" {
				t.Fatalf("a document written through a session's shell: created_by %+v", d.CreatedBy)
			}
		}
	}
	if !found {
		t.Fatal("the session's shell write made no document")
	}
}

func TestWhatWasStoredBeforeAttributionReadsAsUnknownAndKeepsReadingSoAfterARestart(t *testing.T) {
	e := newPOE2E(t)
	tk, err := e.plan.CreateTicket(e.ctx, e.project, "Old task", "", domain.TicketStatusBacklog, "")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := e.plan.CreateDocument(e.ctx, e.project, "Old doc", "x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Rows made before the columns existed hold nothing in them.
	db, err := sqlite.Open(e.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"UPDATE tickets SET created_by = '', changed_by = ''", "UPDATE documents SET created_by = '', changed_by = ''"} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	e.restart()
	row := e.taskRow(tk.ID)
	if row.CreatedBy == nil || row.CreatedBy.Kind != "unknown" || row.ChangedBy != nil {
		t.Fatalf("an old task: created_by %+v changed_by %+v", row.CreatedBy, row.ChangedBy)
	}
	if d := e.documentRow(doc.ID); d.CreatedBy == nil || d.CreatedBy.Kind != "unknown" || d.ChangedBy != nil {
		t.Fatalf("an old document: %+v %+v", d.CreatedBy, d.ChangedBy)
	}
}
