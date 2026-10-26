//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/archrules"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// shellE2E is the server wired as it runs, with the HTTP API and the
// per-session MCP endpoint on a test server, and task sessions whose shells
// the harness keeps in sync (contract "Harness server ↔ Agent sessions —
// virtual shell" §3).
type shellE2E struct {
	t       *testing.T
	ctx     context.Context
	orch    *orchestration.Service
	plan    *planning.Service
	srv     *httptest.Server
	cat     catalog.Catalog
	project string
	repo    string
	// dataDir is the server's data directory.
	dataDir string
	// repoDir is the project's main checkout.
	repoDir string
	cfg     Config
	app     *fx.App
	// sessions, channel, art, shells and rules are what the server was built
	// with, populated again at each boot.
	sessions ports.SessionRepository
	channel  *taskchannel.Service
	art      *artifacts.Service
	shells   *vshell.Service
	rules    *archrules.Service
	notes    *notifications.Service
}

// boot starts the server over e.cfg and fills e's services.
func (e *shellE2E) boot() {
	e.t.Helper()
	e.app = fx.New(
		fx.Supply(e.cfg),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&e.orch, &e.plan, &e.channel, &e.art, &e.cat, &e.sessions, &e.shells, &e.rules, &e.notes),
		fx.NopLogger,
	)
	if err := e.app.Start(e.ctx); err != nil {
		e.t.Fatal(err)
	}
}

// serve puts the HTTP API and the per-session MCP endpoint of the booted
// server on a test server.
func (e *shellE2E) serve() {
	e.t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Sessions: e.orch, Planning: e.plan, TaskResources: e.plan, TaskComments: e.plan, Artifacts: e.art, Shells: e.shells,
		RepositoryArchitectures: e.cat.RepositoryArchitectures, ArchitectureRules: e.rules,
	})))
	mux.Handle(mcpsession.PathPrefix, mcpsession.TaskHandler(sessionTaskTools(e.cat, e.orch, e.plan, e.sessions, e.art, e.channel, nil, nil)))
	e.srv = httptest.NewServer(mux)
	e.t.Cleanup(e.srv.Close)
}

// restart stops the server and starts it again over the same data.
func (e *shellE2E) restart() {
	e.t.Helper()
	if err := e.app.Stop(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	e.srv.Close()
	e.boot()
	e.serve()
}

func newShellE2E(t *testing.T) *shellE2E { return newShellE2EWith(t, ":memory:") }

// newShellE2EWith is newShellE2E over the database at db: a file keeps its
// sessions across restart.
func newShellE2EWith(t *testing.T, db string) *shellE2E {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if db != ":memory:" {
		db = filepath.Join(dir, db)
	}

	e := &shellE2E{t: t, ctx: context.Background(), dataDir: filepath.Join(dir, "data"), repoDir: repoDir}
	e.cfg = Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: db, DataDir: e.dataDir, Root: dir, ClaudeBin: fake, SessionShell: "direct"}
	e.boot()
	t.Cleanup(func() { _ = e.app.Stop(context.Background()) })

	cat := e.cat
	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(e.cfg.Root, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := cat.Projects.CreateProject(e.ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(e.ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	e.project, e.repo = p.ID, r.ID

	e.serve()
	return e
}

func (e *shellE2E) task(title string) string {
	e.t.Helper()
	tk, err := e.plan.CreateTicket(e.ctx, e.project, title, "", domain.TicketStatusInProgress, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return tk.ID
}

// runningSession is a task session the person started in their terminal:
// its id, its worktree and its virtual shell.
type runningSession struct {
	id, worktree, shell string
	// hooks are the settings hooks its CLI was given beside its own.
	hooks map[string][]ports.HookGroup
}

func (e *shellE2E) start(ticketID string) runningSession {
	e.t.Helper()
	return e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: ticketID, Mode: "implement"})
}

// startWith starts an interactive session from req and finds its shell.
func (e *shellE2E) startWith(req ports.InteractiveRequest) runningSession {
	e.t.Helper()
	sess, spec, err := e.orch.StartInteractive(e.ctx, req)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, d := range spec.AddDirs {
		if strings.HasPrefix(filepath.Base(d), "harness-shell-") {
			return runningSession{id: sess.ID, worktree: spec.Dir, shell: d, hooks: spec.Hooks}
		}
	}
	e.t.Fatalf("session %s has no virtual shell in %v", sess.ID, spec.AddDirs)
	return runningSession{}
}

// post calls a JSON route of the API and decodes its answer into out.
func (e *shellE2E) post(path string, body any, out any) {
	e.t.Helper()
	b, _ := json.Marshal(body)
	status, text := postJSON(e.t, e.srv.URL+"/api/"+path, string(b))
	if status/100 != 2 {
		e.t.Fatalf("%s = %d %s", path, status, text)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(text), out); err != nil {
			e.t.Fatalf("%s answer %s: %v", path, text, err)
		}
	}
}

func (e *shellE2E) createDocument(title, content, format, scope string) domain.Document {
	e.t.Helper()
	var out struct {
		Document domain.Document `json:"document"`
	}
	e.post("create_document", map[string]string{"project_id": e.project, "title": title, "content": content, "format": format, "scope": scope}, &out)
	if out.Document.ID == "" {
		e.t.Fatalf("create_document answered no document")
	}
	return out.Document
}

func (e *shellE2E) upload(ticketID, fileName, content string) domain.TicketResource {
	e.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("ticket_id", ticketID)
	part, _ := w.CreateFormFile("file", fileName)
	_, _ = io.WriteString(part, content)
	_ = w.Close()
	res, err := http.Post(e.srv.URL+"/api/upload_ticket_resource", w.FormDataContentType(), &body)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out struct {
		Resource domain.TicketResource `json:"resource"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(b, &out) != nil {
		e.t.Fatalf("upload = %d %s", res.StatusCode, b)
	}
	return out.Resource
}

func (e *shellE2E) dial(s runningSession) *client.Client {
	return dialTask(e.t, e.srv.URL+mcpsession.PathPrefix+s.id)
}

// shellIndex is a shell's .index.json by path.
func shellIndex(dir string) (map[string]ports.ShellFile, error) {
	b, err := os.ReadFile(filepath.Join(dir, ".index.json"))
	if err != nil {
		return nil, err
	}
	var rows []ports.ShellFile
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	out := map[string]ports.ShellFile{}
	for _, r := range rows {
		out[r.Path] = r
	}
	return out, nil
}

// eventually retries check until it passes or five seconds are up: the
// harness rewrites running shells right after the change, not within the
// request that made it.
func eventually(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// hasFile checks the shell holds content at rel, and that its index row
// names id with the given scope and version (version "" skips it).
func hasFile(dir, rel, content, id, scope, version string) error {
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	if string(b) != content {
		return fmt.Errorf("%s holds %q, want %q", rel, b, content)
	}
	idx, err := shellIndex(dir)
	if err != nil {
		return err
	}
	row, ok := idx[rel]
	switch {
	case !ok:
		return fmt.Errorf("%s is not in .index.json", rel)
	case row.ID != id || row.Scope != scope:
		return fmt.Errorf("%s indexed as %+v, want id %s scope %s", rel, row, id, scope)
	case version != "" && row.Version != version:
		return fmt.Errorf("%s indexed at version %s, want %s", rel, row.Version, version)
	}
	return nil
}

// lacks checks the shell has no file with id, on disk or in its index.
func lacks(dir, id string, paths ...string) error {
	idx, err := shellIndex(dir)
	if err != nil {
		return err
	}
	for p, row := range idx {
		if row.ID == id {
			return fmt.Errorf("%s still indexed at %s", id, p)
		}
	}
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			return fmt.Errorf("%s is still on disk", p)
		}
	}
	return nil
}

// fileWith checks the shell's file at rel contains every fragment.
func fileWith(dir, rel string, fragments ...string) error {
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	if !containsAll(string(b), fragments) {
		return fmt.Errorf("%s = %q, want %q", rel, b, fragments)
	}
	return nil
}

// fileWithout checks the shell's file at rel contains none of fragments.
func fileWithout(dir, rel string, fragments ...string) error {
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	for _, f := range fragments {
		if strings.Contains(string(b), f) {
			return fmt.Errorf("%s = %q, still has %q", rel, b, f)
		}
	}
	return nil
}

// A document created, updated, moved between scopes, linked, unlinked or
// deleted through the web API changes in the running shells that should see
// it, and .index.json follows; another task's shell sees the project-scope
// changes and none of this task's.
func TestDocumentChangesReachEveryRunningShell(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Shell sync"), e.task("Elsewhere")
	a, b := e.start(taskA), e.start(taskB)

	// A task document linked to task A shows in A's task/docs only.
	doc := e.createDocument("Plan notes", "# Plan\n\nfirst draft\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the linked task document in A", func() error {
		return hasFile(a.shell, "task/docs/plan-notes.md", "# Plan\n\nfirst draft\n", doc.ID, "task", domain.DocumentVersion("# Plan\n\nfirst draft\n"))
	})

	// An update keeps the file's name even when the title changes.
	e.post("update_document", map[string]string{"document_id": doc.ID, "title": "Renamed plan", "content": "# Plan\n\nsecond draft\n"}, nil)
	eventually(t, "the updated document in A", func() error {
		if err := hasFile(a.shell, "task/docs/plan-notes.md", "# Plan\n\nsecond draft\n", doc.ID, "task", domain.DocumentVersion("# Plan\n\nsecond draft\n")); err != nil {
			return err
		}
		idx, _ := shellIndex(a.shell)
		if got := idx["task/docs/plan-notes.md"].Title; got != "Renamed plan" {
			return fmt.Errorf("indexed title %q", got)
		}
		return nil
	})
	eventually(t, "A's task document under A's folder in B", func() error {
		return hasFile(b.shell, "tasks/"+taskA+"/docs/plan-notes.md", "# Plan\n\nsecond draft\n", doc.ID, "task", "")
	})
	if _, err := os.Stat(filepath.Join(b.shell, "task", "docs", "plan-notes.md")); err == nil {
		t.Fatal("B got A's task document as its own")
	}

	// Moved to project scope, it goes to project/docs, in every shell.
	e.post("set_document_scope", map[string]string{"document_id": doc.ID, "scope": "project"}, nil)
	for name, s := range map[string]runningSession{"A": a, "B": b} {
		eventually(t, "the moved document in "+name, func() error {
			for _, p := range []string{"task/docs/plan-notes.md", "tasks/" + taskA + "/docs/plan-notes.md"} {
				if _, err := os.Stat(filepath.Join(s.shell, filepath.FromSlash(p))); err == nil {
					return fmt.Errorf("%s is still there", p)
				}
			}
			return hasFile(s.shell, "project/docs/renamed-plan.md", "# Plan\n\nsecond draft\n", doc.ID, "project", "")
		})
	}

	// A new project document reaches every shell; deleted, it leaves them.
	proj := e.createDocument("Glossary", "<p>terms</p>", "html", "project")
	for name, s := range map[string]runningSession{"A": a, "B": b} {
		eventually(t, "the new project document in "+name, func() error {
			return hasFile(s.shell, "project/docs/glossary.html", "<p>terms</p>", proj.ID, "project", "")
		})
	}
	e.post("delete_document", map[string]string{"document_id": proj.ID}, nil)
	for name, s := range map[string]runningSession{"A": a, "B": b} {
		eventually(t, "the deleted document gone from "+name, func() error {
			return lacks(s.shell, proj.ID, "project/docs/glossary.html")
		})
	}

	// An unlinked task document shows nowhere until it is linked to task B;
	// unlinked again, it leaves B's shell.
	notes := e.createDocument("B notes", "plain words", "text", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskB, "document_id": notes.ID}, nil)
	eventually(t, "the linked document in B", func() error {
		return hasFile(b.shell, "task/docs/b-notes.txt", "plain words", notes.ID, "task", "")
	})
	eventually(t, "B's task document under B's folder in A", func() error {
		return hasFile(a.shell, "tasks/"+taskB+"/docs/b-notes.txt", "plain words", notes.ID, "task", "")
	})
	e.post("unlink_document_from_ticket", map[string]string{"ticket_id": taskB, "document_id": notes.ID}, nil)
	eventually(t, "the unlinked document gone from B", func() error {
		return lacks(b.shell, notes.ID, "task/docs/b-notes.txt")
	})
	eventually(t, "the unlinked document gone from A", func() error {
		return lacks(a.shell, notes.ID, "tasks/"+taskB+"/docs/b-notes.txt")
	})
}

// A comment and a resource the person adds, and an artifact another
// session publishes, revises, moves and unpublishes through its own shell,
// show in the running shell of the task; another task's shell sees only the
// project assets.
func TestCommentsResourcesAndArtifactsReachEveryRunningShell(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Shell sync"), e.task("Elsewhere")
	a, peer, b := e.start(taskA), e.start(taskA), e.start(taskB)

	var added struct {
		Comment domain.TicketComment `json:"comment"`
	}
	e.post("add_ticket_comment", map[string]string{"ticket_id": taskA, "body": "Please keep names stable."}, &added)
	c := added.Comment
	commentPath := "task/comments/" + c.CreatedAt.UTC().Format("20060102-150405") + "-person-" + c.ID[:8] + ".md"
	eventually(t, "the person's comment in A", func() error {
		return hasFile(a.shell, commentPath, "Please keep names stable.", c.ID, "task", "")
	})
	e.post("update_ticket_comment", map[string]string{"comment_id": c.ID, "body": "Please keep file names stable."}, nil)
	eventually(t, "the edited comment in A", func() error {
		return hasFile(a.shell, commentPath, "Please keep file names stable.", c.ID, "task", "")
	})

	r := e.upload(taskA, "mockup.txt", "the mockup")
	eventually(t, "the uploaded resource in A", func() error {
		return hasFile(a.shell, "task/resources/mockup.txt", "the mockup", r.ID, "task", "")
	})
	e.post("add_ticket_link", map[string]string{"ticket_id": taskA, "url": "https://example.com/spec", "title": "Spec page"}, nil)
	eventually(t, "the link in A's links.md", func() error {
		return fileWith(a.shell, "task/resources/links.md", "[Spec page](https://example.com/spec)")
	})
	if err := lacks(b.shell, c.ID); err != nil {
		t.Fatalf("B saw A's comment: %v", err)
	}
	if err := lacks(b.shell, r.ID); err != nil {
		t.Fatalf("B saw A's resource: %v", err)
	}

	// The peer session publishes a page by writing it in its shell.
	page := "task/designs/design.html"
	e.shellOp(peer, "write", func() { writeShellFile(t, peer, page, "<h1>Design</h1><p>v1</p>") }, page)
	id := indexedID(t, peer, page)
	eventually(t, "the published artifact in A", func() error {
		return hasFile(a.shell, page, "<h1>Design</h1><p>v1</p>", id, "task", "1")
	})
	e.shellOp(peer, "modify", func() { writeShellFile(t, peer, page, "<h1>Design</h1><p>v2</p>") }, page)
	eventually(t, "the revised artifact in A", func() error {
		return hasFile(a.shell, page, "<h1>Design</h1><p>v2</p>", id, "task", "2")
	})
	if err := lacks(b.shell, id); err != nil {
		t.Fatalf("B saw A's task artifact: %v", err)
	}

	// Moved to the project, it is a project asset in every shell; attached
	// to task B, B's attached.md names it.
	asset := "project/designs/design.html"
	e.shellOp(peer, "mv", func() { moveShellFile(t, peer, page, asset) }, page, asset)
	for name, s := range map[string]runningSession{"A": a, "B": b} {
		eventually(t, "the project asset in "+name, func() error {
			if _, err := os.Stat(filepath.Join(s.shell, "task", "designs", "design.html")); err == nil {
				return fmt.Errorf("task/designs/design.html is still there")
			}
			return hasFile(s.shell, asset, "<h1>Design</h1><p>v2</p>", id, "project", "")
		})
	}
	e.post("attach_artifact_to_ticket", map[string]string{"artifact_id": id, "ticket_id": taskB}, nil)
	eventually(t, "the attached asset in B's attached.md", func() error {
		return fileWith(b.shell, "task/designs/attached.md", "Design → project/designs/design.html")
	})
	e.post("detach_artifact_from_ticket", map[string]string{"artifact_id": id, "ticket_id": taskB}, nil)
	eventually(t, "the detached asset gone from B's attached.md", func() error {
		return fileWithout(b.shell, "task/designs/attached.md", "Design →")
	})

	// Back on the task and unpublished, it leaves every shell.
	eventually(t, "the project asset in the peer's shell", func() error {
		return hasFile(peer.shell, asset, "<h1>Design</h1><p>v2</p>", id, "project", "")
	})
	e.shellOp(peer, "mv", func() { moveShellFile(t, peer, asset, page) }, asset, page)
	eventually(t, "the asset back in A's task/designs", func() error {
		return hasFile(a.shell, page, "<h1>Design</h1><p>v2</p>", id, "task", "")
	})
	eventually(t, "the asset gone from B", func() error {
		return lacks(b.shell, id, asset)
	})
	e.shellOp(peer, "rm", func() { removeShellFile(t, peer, page) }, page)
	eventually(t, "the unpublished artifact gone from A", func() error {
		return lacks(a.shell, id, page)
	})

	// The person deletes their comment: it leaves the shell.
	e.post("delete_ticket_comment", map[string]string{"comment_id": c.ID}, nil)
	eventually(t, "the deleted comment gone from A", func() error {
		return lacks(a.shell, c.ID, commentPath)
	})
}
