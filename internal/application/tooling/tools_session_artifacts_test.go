package tooling

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
)

type artifactToolsFixture struct {
	tools    map[string]domain.Tool
	ticketID string
	root     string // sess-1's worktree
	root2    string // sess-2's worktree, same task
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
	tk, err := plan.CreateTicket(context.Background(), proj.ID, "Design the card", "", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	f := &artifactToolsFixture{tools: map[string]domain.Tool{}, ticketID: tk.ID, root: t.TempDir(), root2: t.TempDir()}
	for id, dir := range map[string]string{"sess-1": f.root, "sess-2": f.root2} {
		if _, err := sessions.Create(&domain.Session{ID: id, ProjectID: proj.ID, TicketID: tk.ID, Task: "go", WorkingDir: dir, Status: domain.SessionRunning}); err != nil {
			t.Fatal(err)
		}
	}
	art := artifacts.NewService(sqlite.NewArtifactRepository(db), sessions, nil)
	for _, tl := range SessionTaskTools(plan, sessions, nil, nil, PeerStarter{}, ArtifactTooling{Publisher: art, ViewURL: func(id string) string { return "/api/artifacts/" + id + "/view/" }}) {
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

func TestPublishArtifactTool_PublishesAndReturnsViewURL(t *testing.T) {
	f := newArtifactToolsFixture(t)
	f.write(t, f.root, "design/card.html", "<h1>card</h1>")
	out, err := f.call(t, "sess-1", "publish_artifact", map[string]any{"path": "design/card.html", "title": "Card", "note": "first"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := out["artifact_id"].(string)
	if id == "" || out["revision"] != 1 || out["kind"] != domain.ArtifactPage || out["view_url"] != "/api/artifacts/"+id+"/view/" {
		t.Fatalf("out = %+v", out)
	}
	again, _ := f.call(t, "sess-1", "publish_artifact", map[string]any{"path": "design/card.html", "title": "Card"})
	if again["artifact_id"] != id || again["revision"] != 2 {
		t.Fatalf("re-publish = %+v", again)
	}
	link, err := f.call(t, "sess-1", "publish_artifact", map[string]any{"url": "http://localhost:5173/", "title": "Dev"})
	if err != nil || link["kind"] != domain.ArtifactURL || link["view_url"] != "http://localhost:5173/" {
		t.Fatalf("url publish = %+v, %v", link, err)
	}
}

// The MCP bridge hands the agent only the message, so the contract's codes
// travel in it.
func TestPublishArtifactTool_ErrorsCarryTheirCode(t *testing.T) {
	f := newArtifactToolsFixture(t)
	for name, args := range map[string]map[string]any{
		"ARTIFACT_NOT_FOUND":             {"path": "design/nope.html", "title": "T"},
		"ARTIFACT_PATH_OUTSIDE_WORKTREE": {"path": "../../etc/passwd", "title": "T"},
		"ARTIFACT_URL_NOT_LOCAL":         {"url": "http://example.com", "title": "T"},
	} {
		_, err := f.call(t, "sess-1", "publish_artifact", args)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		var se *domain.StructuredError
		if !errorsAs(err, &se) || se.Code != name || !strings.HasPrefix(se.Message, name+": ") {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	// Non-artifact codes are left alone.
	_, err := f.call(t, "sess-1", "publish_artifact", map[string]any{"path": "x"})
	var se *domain.StructuredError
	if !errorsAs(err, &se) || se.Code != "INVALID_INPUT" || strings.HasPrefix(se.Message, "INVALID_INPUT") {
		t.Fatalf("err = %v", err)
	}
}

func TestListAndUnpublishArtifactTools(t *testing.T) {
	f := newArtifactToolsFixture(t)
	f.write(t, f.root, "a.html", "a")
	f.write(t, f.root2, "b.png", "\x89PNG")
	a, _ := f.call(t, "sess-1", "publish_artifact", map[string]any{"path": "a.html", "title": "A"})
	time.Sleep(2 * time.Millisecond) // millisecond timestamps: keep "newest first" unambiguous
	b, _ := f.call(t, "sess-2", "publish_artifact", map[string]any{"path": "b.png", "title": "B"})

	listed, err := f.call(t, "sess-1", "list_task_artifacts", nil)
	if err != nil {
		t.Fatal(err)
	}
	items := listed["artifacts"].([]taskArtifact)
	if listed["count"] != 2 || len(items) != 2 || items[0].ArtifactID != b["artifact_id"] || items[0].SessionID != "sess-2" || items[1].ArtifactID != a["artifact_id"] || items[1].Path != "a.html" {
		t.Fatalf("list = %+v", items)
	}

	if _, err := f.call(t, "sess-1", "unpublish_artifact", map[string]any{"artifact_id": b["artifact_id"]}); err == nil || !strings.Contains(err.Error(), "ARTIFACT_NOT_YOURS") {
		t.Fatalf("unpublishing another session's artifact = %v", err)
	}
	if _, err := f.call(t, "sess-1", "unpublish_artifact", nil); err == nil || !strings.Contains(err.Error(), "INVALID_INPUT") {
		t.Fatalf("missing id = %v", err)
	}
	out, err := f.call(t, "sess-1", "unpublish_artifact", map[string]any{"artifact_id": a["artifact_id"]})
	if err != nil || out["removed"] != true {
		t.Fatalf("unpublish = %+v, %v", out, err)
	}
	listed, _ = f.call(t, "sess-1", "list_task_artifacts", nil)
	if listed["count"] != 1 {
		t.Fatalf("after unpublish: %+v", listed)
	}
}

func TestArtifactTools_UnavailableWithoutAPublisher(t *testing.T) {
	f := newTaskToolsFixture(t) // built with a zero ArtifactTooling
	for _, name := range []string{"publish_artifact", "list_task_artifacts", "unpublish_artifact"} {
		_, err := f.call(t, f.sessionID, name, map[string]any{"title": "T", "path": "x", "artifact_id": "y"})
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
