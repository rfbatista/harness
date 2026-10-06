package tooling

import (
	"context"
	"errors"
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
		"title": "Plan", "content": "# Plan\nstep one",
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
	if got := read.(map[string]any)["document"].(*domain.Document).Content; got != "# Plan\nstep one" {
		t.Fatalf("content = %q", got)
	}
}

// An omitted field keeps its stored value, so an agent can revise content
// without restating the title.
func TestSessionTaskTools_UpdateIsPartial(t *testing.T) {
	f := newTaskToolsFixture(t)
	out, err := f.call(t, f.sessionID, "create_task_document", map[string]any{"title": "Plan", "content": "old"})
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["document"].(*domain.Document).ID

	updated, err := f.call(t, f.sessionID, "update_task_document", map[string]any{
		"document_id": id, "content": "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := updated.(map[string]any)["document"].(*domain.Document)
	if doc.Title != "Plan" || doc.Content != "new" {
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
	doc, err := f.plan.CreateDocument(f.projectID, "Secret", "not yours")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.plan.LinkDocument(other.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	_, err = f.call(t, f.sessionID, "read_task_document", map[string]any{"document_id": doc.ID})
	wantCode(t, err, "DOCUMENT_NOT_ON_TASK")

	_, err = f.call(t, f.sessionID, "update_task_document", map[string]any{"document_id": doc.ID, "content": "x"})
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
