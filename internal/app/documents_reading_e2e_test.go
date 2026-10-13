package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// readingMCP is the wired server as a task session reaches it over MCP: a
// project, the session's task, a second task, and the session's client.
type readingMCP struct {
	plan     *planning.Service
	art      *artifacts.Service
	sessions ports.SessionRepository
	project  *domain.Project
	task     *domain.Ticket
	other    *domain.Ticket
	worktree string
	agent    *client.Client
}

func newReadingMCP(t *testing.T) *readingMCP {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		plan     *planning.Service
		orch     *orchestration.Service
		sessions ports.SessionRepository
		channel  *taskchannel.Service
		art      *artifacts.Service
	)
	fxApp := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &plan, &orch, &sessions, &channel, &art),
		fx.NopLogger,
	)
	if err := fxApp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fxApp.Stop(context.Background()) })

	p, err := cat.Projects.CreateProject(ctx, "harness", dir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(ctx, p.ID, "Task", "", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	other, err := plan.CreateTicket(ctx, p.ID, "Other task", "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	if _, err := sessions.Create(&domain.Session{ID: "s-reader", ProjectID: p.ID, TicketID: tk.ID, Task: "read",
		Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerTUI, WorkingDir: worktree}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(mcpsession.PathPrefix, mcpsession.TaskHandler(sessionTaskTools(cat, orch, plan, sessions, art, channel, nil)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &readingMCP{plan: plan, art: art, sessions: sessions, project: p, task: tk, other: other, worktree: worktree,
		agent: dialTask(t, srv.URL+mcpsession.PathPrefix+"s-reader")}
}

// taskDocument stores an HTML page linked to the session's task.
func (r *readingMCP) taskDocument(t *testing.T, title, body string) *domain.Document {
	t.Helper()
	d, err := r.plan.CreateDocument(r.project.ID, title, "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>"+title+
		"</title><style>body{font:15px system-ui}</style></head><body>\n"+body+"\n</body></html>\n", domain.DocumentFormatHTML, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.plan.LinkDocument(r.task.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	return d
}

// call is callTask decoded as JSON.
func (r *readingMCP) call(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(callTask(t, r.agent, tool, args)), &out); err != nil {
		t.Fatalf("%s answered non-JSON: %v", tool, err)
	}
	return out
}

func texts(m map[string]any) []string {
	var out []string
	for _, l := range m["lines"].([]any) {
		out = append(out, l.(map[string]any)["text"].(string))
	}
	return out
}

// bigPage is about 60 KB of HTML: an intro, then sections of rows.
func bigPage() string {
	var b strings.Builder
	b.WriteString("<h1>System architecture</h1><p>How the harness is built.</p>\n")
	for s := 1; s <= 12; s++ {
		b.WriteString("<h2>Context " + string(rune('A'+s-1)) + "</h2>\n<table>\n")
		for r := 1; r <= 40; r++ {
			b.WriteString("<tr><td>row</td><td>" + strings.Repeat("detail ", 15) + "</td></tr>\n")
		}
		b.WriteString("</table>\n")
	}
	b.WriteString(`<h2 id="add">Where new code goes</h2><p>internal/application/tooling for MCP tools.</p>`)
	return b.String()
}

// An agent reads a 60 KB page over MCP in parts: the outline and a first
// window instead of the page, then a section, then the next window. A small
// page still comes back whole. Refusals arrive as coded messages, the only
// thing the MCP bridge hands an agent.
func TestReadingDocumentsInParts_EndToEnd(t *testing.T) {
	r := newReadingMCP(t)
	big := r.taskDocument(t, "System architecture", bigPage())
	if len(big.Content) < 60_000 {
		t.Fatalf("the big page is only %d bytes", len(big.Content))
	}

	// The listing says it is big before the agent opens it.
	listed := r.call(t, "list_task_documents", nil)
	row := listed["documents"].([]any)[0].(map[string]any)
	if row["bytes"] != float64(len(big.Content)) || row["lines"].(float64) < 400 || row["version"] != domain.DocumentVersion(big.Content) {
		t.Fatalf("listing row: %v", row)
	}

	first := r.call(t, "read_task_document", map[string]any{"document_id": big.ID})
	if _, ok := first["document"].(map[string]any)["content"]; ok {
		t.Fatal("a 60 KB page came back whole")
	}
	if first["truncated"] != true || first["next_offset"] != float64(201) || len(texts(first)) != 200 {
		t.Fatalf("first window: truncated %v next %v lines %d", first["truncated"], first["next_offset"], len(texts(first)))
	}
	outline := first["outline"].([]any)
	last := outline[len(outline)-1].(map[string]any)
	if len(outline) != 14 || last["section_id"] != "add" || last["heading"] != "Where new code goes" {
		t.Fatalf("outline: %d entries, last %v", len(outline), last)
	}

	section := r.call(t, "read_task_document", map[string]any{"document_id": big.ID, "section": "add"})
	if got := texts(section); len(got) != 2 || got[0] != "## Where new code goes" || got[1] != "internal/application/tooling for MCP tools." {
		t.Fatalf("section: %q", got)
	}
	next := r.call(t, "read_task_document", map[string]any{"document_id": big.ID, "offset": first["next_offset"], "limit": 5})
	if n := next["lines"].([]any)[0].(map[string]any)["n"]; n != float64(201) || len(texts(next)) != 5 {
		t.Fatalf("next window starts at %v", n)
	}

	small := r.taskDocument(t, "Note", "<p>short</p>")
	whole := r.call(t, "read_task_document", map[string]any{"document_id": small.ID})
	if doc := whole["document"].(map[string]any); doc["content"] != small.Content || doc["lines"] != float64(1) {
		t.Fatalf("small page: %v", doc)
	}

	theirs, _ := r.plan.CreateDocument(r.project.ID, "Theirs", "<!doctype html><html><body>x</body></html>", domain.DocumentFormatHTML, domain.DocumentScopeTask)
	_ = r.plan.LinkDocument(r.other.ID, theirs.ID)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"document_id": big.ID, "section": "nowhere"}, "SECTION_NOT_FOUND: no section \"nowhere\"; sections: 1, 1.1,"},
		{map[string]any{"document_id": big.ID, "section": "add", "view": "raw"}, "INVALID_INPUT: section reads use the text view"},
		{map[string]any{"document_id": big.ID, "limit": 5000}, "INVALID_INPUT: limit must be"},
		{map[string]any{"document_id": theirs.ID}, "DOCUMENT_NOT_ON_TASK: "},
	} {
		if got := callTaskErr(t, r.agent, "read_task_document", c.args); !strings.HasPrefix(got, c.want) {
			t.Fatalf("%v: message %q, want it to start with %q", c.args, got, c.want)
		}
	}
}

// An agent finds a fact without opening any page: search names the
// document, the line and the section, and a one-line window at that line
// reads the same text. A bad regex comes back as a coded message.
func TestSearchingDocuments_EndToEnd(t *testing.T) {
	r := newReadingMCP(t)
	big := r.taskDocument(t, "System architecture", bigPage())
	conv, err := r.plan.CreateDocument(r.project.ID, "Conventions", "# Conventions\n\nErrors start with their code.\n", domain.DocumentFormatMarkdown, domain.DocumentScopeProject)
	if err != nil {
		t.Fatal(err)
	}

	found := r.call(t, "search_documents", map[string]any{"pattern": "where new code goes"})
	matches := found["matches"].([]any)
	if len(matches) != 1 || found["searched"] != float64(2) || found["truncated"] != false {
		t.Fatalf("search: %v", found)
	}
	m := matches[0].(map[string]any)
	if m["id"] != big.ID || m["source"] != "task_document" || m["section_id"] != "add" || m["text"] != "## Where new code goes" {
		t.Fatalf("match: %v", m)
	}
	at := r.call(t, "read_task_document", map[string]any{"document_id": big.ID, "offset": m["line"], "limit": 1})
	if got := texts(at); len(got) != 1 || got[0] != m["text"] {
		t.Fatalf("the window at the hit reads %q", got)
	}

	code := r.call(t, "search_documents", map[string]any{"pattern": `start with their \w+`, "regex": true, "scope": "project"})
	if ms := code["matches"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != conv.ID || ms[0].(map[string]any)["source"] != "project_document" {
		t.Fatalf("project search: %v", code)
	}

	if got := callTaskErr(t, r.agent, "search_documents", map[string]any{"pattern": "(unclosed", "regex": true}); !strings.HasPrefix(got, "INVALID_PATTERN: ") {
		t.Fatalf("bad regex: %q", got)
	}
}
