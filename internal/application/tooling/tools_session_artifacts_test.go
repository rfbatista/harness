package tooling

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type artifactToolsFixture struct {
	tools         map[string]domain.Tool
	art           *artifacts.Service
	ticketID      string
	otherTicketID string // sess-3's task
	root          string // sess-1's worktree
	root2         string // sess-2's worktree, same task
	root3         string // sess-3's worktree, another task of the same project
	root4         string // sess-4's worktree, a task of another project
}

func newArtifactToolsFixture(t *testing.T) *artifactToolsFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, _ := projects.Create("p", t.TempDir())
	tk, err := plan.CreateTicket(context.Background(), proj.ID, "Design the card", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := plan.CreateTicket(context.Background(), proj.ID, "Design the logo", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	proj2, _ := projects.Create("p2", t.TempDir())
	foreign, err := plan.CreateTicket(context.Background(), proj2.ID, "Elsewhere", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	f := &artifactToolsFixture{tools: map[string]domain.Tool{}, ticketID: tk.ID, root: t.TempDir(), root2: t.TempDir(), root3: t.TempDir(), root4: t.TempDir()}
	for _, s := range []*domain.Session{
		{ID: "sess-1", ProjectID: proj.ID, TicketID: tk.ID, WorkingDir: f.root},
		{ID: "sess-2", ProjectID: proj.ID, TicketID: tk.ID, WorkingDir: f.root2},
		{ID: "sess-3", ProjectID: proj.ID, TicketID: other.ID, WorkingDir: f.root3},
		{ID: "sess-4", ProjectID: proj2.ID, TicketID: foreign.ID, WorkingDir: f.root4},
	} {
		s.Task, s.Status = "go", domain.SessionRunning
		if _, err := sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	art := artifacts.NewService(sqlite.NewArtifactRepository(db), sessions, nil)
	art.StoreDir = t.TempDir() // moving to project copies the bytes here
	art.Tickets = plan
	f.otherTicketID = other.ID
	f.art = art
	tooling := ArtifactTooling{Publisher: art, Attachments: art, ViewURL: func(id string) string { return "/api/artifacts/" + id + "/view/" }}
	for _, tl := range SessionTaskTools(plan, sessions, nil, nil, PeerStarter{}, tooling, nil, NotificationTooling{}) {
		f.tools[tl.Name] = tl
	}
	return f
}

func (f *artifactToolsFixture) call(t *testing.T, sessionID, tool string, args map[string]any) (map[string]any, error) {
	t.Helper()
	out, err := f.tools[tool].Handler(WithSessionID(context.Background(), sessionID), args)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

func (f *artifactToolsFixture) write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// publish publishes path from session's worktree through the artifacts
// service, as the shell does for a file in task/designs, and returns its id.
func (f *artifactToolsFixture) publish(t *testing.T, session, root, path string) string {
	t.Helper()
	f.write(t, root, path, "<h1>"+path+"</h1>")
	a, err := f.art.Publish(context.Background(), ports.PublishArtifactRequest{SessionID: session, Path: path, Title: path})
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// A dev server's loopback URL is published, and publishing it again revises
// the same card.
func TestPublishArtifactTool_PublishesALoopbackURL(t *testing.T) {
	f := newArtifactToolsFixture(t)
	out, err := f.call(t, "sess-1", "publish_artifact", map[string]any{"url": "http://localhost:5173/", "title": "Dev", "note": "first"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := out["artifact_id"].(string)
	if id == "" || out["revision"] != 1 || out["kind"] != domain.ArtifactURL || out["view_url"] != "http://localhost:5173/" {
		t.Fatalf("out = %+v", out)
	}
	again, err := f.call(t, "sess-1", "publish_artifact", map[string]any{"url": "http://localhost:5173/", "title": "Dev"})
	if err != nil || again["artifact_id"] != id || again["revision"] != 2 {
		t.Fatalf("re-publish = %+v, %v", again, err)
	}
}

// A file is published through the shell, not the tool: a path is refused
// with a code that says where to put it, and nothing is published.
func TestPublishArtifactTool_RefusesAFilePath(t *testing.T) {
	f := newArtifactToolsFixture(t)
	f.write(t, f.root, "design/card.html", "<h1>card</h1>")
	for _, args := range []map[string]any{
		{"path": "design/card.html", "title": "Card"},
		{"path": "design/card.html", "url": "http://localhost:5173/", "title": "Card"},
	} {
		_, err := f.call(t, "sess-1", "publish_artifact", args)
		var se *domain.StructuredError
		if !errorsAs(err, &se) || se.Code != "PUBLISH_URL_ONLY" || !strings.HasPrefix(se.Message, "PUBLISH_URL_ONLY: ") || !strings.Contains(se.Message, "task/designs/") {
			t.Fatalf("%v: err = %v", args, err)
		}
	}
	if list, _ := f.art.ListTaskArtifacts(context.Background(), f.ticketID); len(list) != 0 {
		t.Fatalf("a refused publish stored %+v", list)
	}
}

// The MCP bridge hands the agent only the message, so the codes travel in it.
func TestPublishArtifactTool_ErrorsCarryTheirCode(t *testing.T) {
	f := newArtifactToolsFixture(t)
	for code, args := range map[string]map[string]any{
		"ARTIFACT_URL_NOT_LOCAL": {"url": "http://example.com", "title": "T"},
		"INVALID_INPUT":          {"title": "T"},
	} {
		_, err := f.call(t, "sess-1", "publish_artifact", args)
		var se *domain.StructuredError
		if !errorsAs(err, &se) || se.Code != code || !strings.HasPrefix(se.Message, code+": ") || strings.Count(se.Message, code) != 1 {
			t.Fatalf("%s: err = %v", code, err)
		}
	}
}

func TestArtifactTools_UnavailableWithoutAPublisher(t *testing.T) {
	f := newTaskToolsFixture(t) // built with a zero ArtifactTooling
	for _, name := range []string{"publish_artifact", "attach_artifact_to_task", "detach_artifact_from_task"} {
		_, err := f.call(t, f.sessionID, name, map[string]any{"title": "T", "url": "http://localhost:5173/", "artifact_id": "y"})
		wantCode(t, err, "UNAVAILABLE")
	}
	// Scope still fails closed first.
	_, err := f.call(t, "", "publish_artifact", nil)
	wantCode(t, err, "SESSION_NOT_FOUND")
}

func errorsAs(err error, target **domain.StructuredError) bool {
	se, ok := err.(*domain.StructuredError)
	if ok {
		*target = se
	}
	return ok
}
