package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
)

type taskToolsFixture struct {
	tools     map[string]domain.Tool
	plan      *planning.Service
	projectID string
	ticketID  string
	sessionID string
}

// newTaskToolsFixture builds a project with one ticket and a session spawned
// into it, plus a second ticket to check the scope boundary.
func newTaskToolsFixture(t *testing.T) *taskToolsFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	tickets := sqlite.NewTicketRepository(db)
	documents := sqlite.NewDocumentRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(tickets, documents, projects)

	proj, err := projects.Create("p", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(context.Background(), proj.ID, "Ship the thing", "with care", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(&domain.Session{
		ID: "sess-1", ProjectID: proj.ID, TicketID: tk.ID, Task: "go", Status: domain.SessionRunning,
	}); err != nil {
		t.Fatal(err)
	}

	byName := map[string]domain.Tool{}
	for _, tool := range SessionTaskTools(plan, sessions, nil, nil, PeerStarter{}, ArtifactTooling{}) {
		byName[tool.Name] = tool
	}
	if len(byName) != len(SessionTaskToolNames) {
		t.Fatalf("got %d tools, SessionTaskToolNames lists %d", len(byName), len(SessionTaskToolNames))
	}
	for _, name := range SessionTaskToolNames {
		if _, ok := byName[name]; !ok {
			t.Fatalf("tool %q named in SessionTaskToolNames but not built", name)
		}
	}

	return &taskToolsFixture{tools: byName, plan: plan, projectID: proj.ID, ticketID: tk.ID, sessionID: "sess-1"}
}

func (f *taskToolsFixture) call(t *testing.T, sessionID, tool string, args map[string]any) (any, error) {
	t.Helper()
	ctx := context.Background()
	if sessionID != "" {
		ctx = WithSessionID(ctx, sessionID)
	}
	return f.tools[tool].Handler(ctx, args)
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("want %s, got %v", code, err)
	}
	if se.Code != code {
		t.Fatalf("want %s, got %s (%s)", code, se.Code, se.Message)
	}
}

func TestSessionTaskTools_GetTask(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "get_task", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	tk := m["task"].(*domain.Ticket)
	if tk.ID != f.ticketID || tk.Title != "Ship the thing" {
		t.Fatalf("wrong task: %+v", tk)
	}
	if docs := m["documents"].([]documentSummary); len(docs) != 0 {
		t.Fatalf("want no documents yet, got %d", len(docs))
	}
}

// The whole point of the surface: what one session writes, the next one reads.
func TestSessionTaskTools_CreateThenList(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "create_task_document", map[string]any{
		"title": "Plan", "content": htmlPage("Plan", "step one"),
	})
	if err != nil {
		t.Fatal(err)
	}
	created := out.(map[string]any)["document"].(*domain.Document)

	listed, err := f.call(t, f.sessionID, "list_task_documents", nil)
	if err != nil {
		t.Fatal(err)
	}
	docs := listed.(map[string]any)["documents"].([]documentSummary)
	if len(docs) != 1 || docs[0].ID != created.ID || docs[0].Title != "Plan" {
		t.Fatalf("document not linked to the task: %+v", docs)
	}

	read, err := f.call(t, f.sessionID, "read_task_document", map[string]any{"document_id": created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := read.(map[string]any)["document"].(*domain.Document); got.Content != htmlPage("Plan", "step one") || got.Format != domain.DocumentFormatHTML {
		t.Fatalf("read back %+v", got)
	}
}

// An omitted field keeps its stored value, so an agent can revise content
// without restating the title.
func TestSessionTaskTools_UpdateIsPartial(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "create_task_document", map[string]any{"title": "Plan", "content": htmlPage("Plan", "old")})
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["document"].(*domain.Document).ID

	updated, err := f.call(t, f.sessionID, "update_task_document", map[string]any{
		"document_id": id, "content": htmlPage("Plan", "new"),
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := updated.(map[string]any)["document"].(*domain.Document)
	if doc.Title != "Plan" || doc.Content != htmlPage("Plan", "new") {
		t.Fatalf("partial update lost a field: %+v", doc)
	}
}

// A document on another task is out of reach even with a valid id in hand.
func TestSessionTaskTools_OtherTaskDocumentIsUnreachable(t *testing.T) {
	f := newTaskToolsFixture(t)
	other, err := f.plan.CreateTicket(context.Background(), f.projectID, "Elsewhere", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := f.plan.CreateDocument(f.projectID, "Secret", "not yours", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.plan.LinkDocument(other.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	_, err = f.call(t, f.sessionID, "read_task_document", map[string]any{"document_id": doc.ID})
	wantCode(t, err, "DOCUMENT_NOT_ON_TASK")

	_, err = f.call(t, f.sessionID, "update_task_document", map[string]any{"document_id": doc.ID, "content": htmlPage("Secret", "x")})
	wantCode(t, err, "DOCUMENT_NOT_ON_TASK")
}

func TestSessionTaskTools_ScopeErrors(t *testing.T) {
	f := newTaskToolsFixture(t)

	_, err := f.call(t, "", "get_task", nil)
	wantCode(t, err, "SESSION_NOT_FOUND")

	_, err = f.call(t, "nope", "get_task", nil)
	wantCode(t, err, "SESSION_NOT_FOUND")

	_, err = f.call(t, f.sessionID, "read_task_document", map[string]any{})
	wantCode(t, err, "INVALID_INPUT")
}

func TestSessionTaskTools_SessionWithoutTask(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, err := projects.Create("p", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(&domain.Session{ID: "free", ProjectID: proj.ID, Task: "go", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	tools := SessionTaskTools(plan, sessions, nil, nil, PeerStarter{}, ArtifactTooling{})

	_, err = tools[0].Handler(WithSessionID(context.Background(), "free"), nil)
	wantCode(t, err, "SESSION_HAS_NO_TASK")
}

// An agent moves its own task as the work moves; the change is what get_task
// shows next, in this session and in every other one on the task.
func TestSessionTaskTools_UpdateTaskStatus(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "update_task_status", map[string]any{"status": "review"})
	if err != nil {
		t.Fatal(err)
	}
	tk := out.(map[string]any)["task"].(*domain.Ticket)
	if tk.ID != f.ticketID || tk.Status != domain.TicketStatusReview || tk.Title != "Ship the thing" || tk.Description != "with care" {
		t.Fatalf("status change touched more than the status: %+v", tk)
	}
	got, err := f.call(t, f.sessionID, "get_task", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.(map[string]any)["task"].(*domain.Ticket).Status != domain.TicketStatusReview {
		t.Fatal("get_task does not see the new status")
	}

	// The same status again succeeds and changes nothing.
	again, err := f.call(t, f.sessionID, "update_task_status", map[string]any{"status": "review"})
	if err != nil {
		t.Fatal(err)
	}
	if a := again.(map[string]any)["task"].(*domain.Ticket); !a.UpdatedAt.Equal(tk.UpdatedAt) {
		t.Fatalf("idempotent call wrote: %+v then %+v", tk, a)
	}
}

func TestSessionTaskTools_UpdateTaskStatus_Input(t *testing.T) {
	f := newTaskToolsFixture(t)
	_, err := f.call(t, f.sessionID, "update_task_status", map[string]any{})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, f.sessionID, "update_task_status", map[string]any{"status": ""})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, f.sessionID, "update_task_status", map[string]any{"status": "shipped"})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, f.sessionID, "update_task_status", map[string]any{"status": 3})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, "", "update_task_status", map[string]any{"status": "done"})
	wantCode(t, err, "SESSION_NOT_FOUND")
}

// There is no task id to pass: one smuggled into the arguments is ignored and
// the session's own task is the one that moves.
func TestSessionTaskTools_UpdateTaskStatus_OnlyItsOwnTask(t *testing.T) {
	f := newTaskToolsFixture(t)
	other, err := f.plan.CreateTicket(context.Background(), f.projectID, "Elsewhere", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.call(t, f.sessionID, "update_task_status", map[string]any{"status": "done", "task_id": other.ID, "ticket_id": other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["task"].(*domain.Ticket).ID != f.ticketID {
		t.Fatal("moved a task other than the session's own")
	}
	stillThere, _ := f.plan.GetTicket(context.Background(), other.ID)
	if stillThere.Status != domain.TicketStatusTodo {
		t.Fatalf("the other task moved: %+v", stillThere)
	}
}

// The task may be deleted while the session runs; the tool then says so.
func TestSessionTaskTools_UpdateTaskStatus_DeletedTask(t *testing.T) {
	f := newTaskToolsFixture(t)
	if err := f.plan.DeleteTicket(context.Background(), f.ticketID); err != nil {
		t.Fatal(err)
	}
	_, err := f.call(t, f.sessionID, "update_task_status", map[string]any{"status": "done"})
	wantCode(t, err, "TICKET_NOT_FOUND")
}

// htmlPage is the minimal document the contract requires.
func htmlPage(title, body string) string {
	return "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>" + title + "</title></head><body>" + body + "</body></html>"
}

func TestSessionTaskTools_CreateRefusesNonHTML(t *testing.T) {
	f := newTaskToolsFixture(t)
	for _, content := range []string{"# Plan\nstep one", "<h1>Plan</h1>", ""} {
		_, err := f.call(t, f.sessionID, "create_task_document", map[string]any{"title": "Plan", "content": content})
		wantCode(t, err, "DOCUMENT_NOT_HTML")
		var se *domain.StructuredError
		errors.As(err, &se)
		if !strings.Contains(se.Message, "<!doctype html>") {
			t.Errorf("the refusal does not say what to send instead: %q", se.Message)
		}
	}
	// Nothing was stored.
	listed, err := f.call(t, f.sessionID, "list_task_documents", nil)
	if err != nil {
		t.Fatal(err)
	}
	if docs := listed.(map[string]any)["documents"].([]documentSummary); len(docs) != 0 {
		t.Fatalf("a refused document was stored: %+v", docs)
	}
	if all := f.plan.ListDocuments(f.projectID); len(all) != 0 {
		t.Fatalf("a refused document exists unlinked in the project: %+v", all)
	}
}

func TestSessionTaskTools_CreateWritesHTMLAndSummariesCarryFormat(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "create_task_document", map[string]any{"title": "Plan", "content": "\uFEFF  <!DOCTYPE HTML>" + htmlPage("Plan", "x")[15:]})
	if err != nil {
		t.Fatal(err)
	}
	if doc := out.(map[string]any)["document"].(*domain.Document); doc.Format != domain.DocumentFormatHTML {
		t.Fatalf("format = %q", doc.Format)
	}
	got, err := f.call(t, f.sessionID, "get_task", nil)
	if err != nil {
		t.Fatal(err)
	}
	docs := got.(map[string]any)["documents"].([]documentSummary)
	if len(docs) != 1 || docs[0].Format != domain.DocumentFormatHTML {
		t.Fatalf("summary lacks the format: %+v", docs)
	}
	b, _ := json.Marshal(docs[0])
	if !strings.Contains(string(b), `"format":"html"`) {
		t.Fatalf("summary JSON lacks format: %s", b)
	}
}

func TestSessionTaskTools_UpdateRefusesNonHTMLAndKeepsTheOld(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, _ := f.call(t, f.sessionID, "create_task_document", map[string]any{"title": "Plan", "content": htmlPage("Plan", "old")})
	id := out.(map[string]any)["document"].(*domain.Document).ID
	_, err := f.call(t, f.sessionID, "update_task_document", map[string]any{"document_id": id, "content": "# new"})
	wantCode(t, err, "DOCUMENT_NOT_HTML")
	read, _ := f.call(t, f.sessionID, "read_task_document", map[string]any{"document_id": id})
	if got := read.(map[string]any)["document"].(*domain.Document); got.Content != htmlPage("Plan", "old") || got.Format != domain.DocumentFormatHTML {
		t.Fatalf("a refused update changed the document: %+v", got)
	}
}

// A document written before this change is Markdown. Retitling it keeps it
// so; giving it new content makes it an HTML page.
func TestSessionTaskTools_UpdateTitleOnlyKeepsFormat(t *testing.T) {
	f := newTaskToolsFixture(t)
	legacy, err := f.plan.CreateDocument(f.projectID, "Old notes", "# old", domain.DocumentFormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.plan.LinkDocument(f.ticketID, legacy.ID); err != nil {
		t.Fatal(err)
	}
	out, err := f.call(t, f.sessionID, "update_task_document", map[string]any{"document_id": legacy.ID, "title": "Older notes"})
	if err != nil {
		t.Fatal(err)
	}
	if doc := out.(map[string]any)["document"].(*domain.Document); doc.Format != domain.DocumentFormatMarkdown || doc.Content != "# old" || doc.Title != "Older notes" {
		t.Fatalf("title-only update touched more than the title: %+v", doc)
	}
}

func TestSessionTaskTools_UpdateContentFlipsLegacyToHTML(t *testing.T) {
	f := newTaskToolsFixture(t)
	legacy, _ := f.plan.CreateDocument(f.projectID, "Old notes", "# old", domain.DocumentFormatMarkdown)
	_ = f.plan.LinkDocument(f.ticketID, legacy.ID)
	out, err := f.call(t, f.sessionID, "update_task_document", map[string]any{"document_id": legacy.ID, "content": htmlPage("Old notes", "new")})
	if err != nil {
		t.Fatal(err)
	}
	if doc := out.(map[string]any)["document"].(*domain.Document); doc.Format != domain.DocumentFormatHTML {
		t.Fatalf("new content did not flip the format: %+v", doc)
	}
}

func TestSessionTaskTools_DescriptionsSayHTML(t *testing.T) {
	f := newTaskToolsFixture(t)
	for _, name := range []string{"create_task_document", "update_task_document", "read_task_document"} {
		tl := f.tools[name]
		schema, _ := json.Marshal(tl.InputSchema)
		text := strings.ToLower(tl.Description + string(schema))
		// The writers may mention Markdown only to refuse it; the reader may
		// say older documents are Markdown.
		if strings.Contains(text, "markdown") && !strings.Contains(text, "markdown is refused") && !strings.Contains(text, "markdown for older documents") {
			t.Errorf("%s still describes Markdown: %q", name, text)
		}
		if !strings.Contains(text, "html") {
			t.Errorf("%s does not say HTML: %q", name, text)
		}
	}
}
