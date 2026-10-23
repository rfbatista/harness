package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/githubapi/githubapitest"
	"operators-mcp/internal/domain"
)

// mergeFixture is the wired server on a real git repository: a checkout on
// main whose origin is a local bare repository, so a test can prove that
// nothing was pushed, and a task whose sessions each work on a branch in their
// own worktree.
type mergeFixture struct {
	*prServer
	dir      string
	checkout string // the repository's local checkout, on main
	bare     string // its origin
	project  *domain.Project
	repo     *domain.Repository
	task     *domain.Ticket
}

func startMergeFixture(t *testing.T) *mergeFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	f := &mergeFixture{dir: dir, checkout: filepath.Join(dir, "widgets"), bare: filepath.Join(dir, "origin.git")}
	gitInit(t, f.checkout)
	gitE2E(t, f.checkout, "config", "user.name", "Person")
	gitE2E(t, f.checkout, "config", "user.email", "person@test")
	if err := os.WriteFile(filepath.Join(f.checkout, "README.md"), []byte("widgets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, f.checkout, "add", ".")
	gitE2E(t, f.checkout, "commit", "-q", "-m", "readme")
	gitE2E(t, dir, "init", "-q", "--bare", f.bare)
	gitE2E(t, f.checkout, "remote", "add", "origin", f.bare)
	gitE2E(t, f.checkout, "push", "-q", "origin", "main")
	gitE2E(t, f.checkout, "fetch", "-q", "origin")

	f.prServer = startPRServer(t, dir, filepath.Join(dir, "harness.db"), githubapitest.New())
	t.Cleanup(func() { _ = f.app.Stop(context.Background()) })
	if _, err := f.cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	var err error
	if f.project, err = f.cat.Projects.CreateProject(ctx, "widgets", f.checkout); err != nil {
		t.Fatal(err)
	}
	if f.repo, err = f.cat.Projects.CreateRepository(ctx, f.project.ID, "widgets", "", "file://"+f.bare, f.checkout); err != nil {
		t.Fatal(err)
	}
	if f.task, err = f.plan.CreateTicket(ctx, f.project.ID, "Merge a branch", "Merge it from the UI", domain.TicketStatusInProgress, ""); err != nil {
		t.Fatal(err)
	}
	return f
}

// session is a finished session on its own branch (agent/<id>, cut from main),
// with one commit per file given, in its worktree.
func (f *mergeFixture) session(t *testing.T, id string, files map[string]string) *domain.Workspace {
	t.Helper()
	ws, err := f.ws.Create(f.repo.ID, id, "agent/"+id, "main")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(ws.Path, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitE2E(t, ws.Path, "add", ".")
		gitE2E(t, ws.Path, "commit", "-q", "-m", "Add "+name)
	}
	if _, err := f.sess.Create(&domain.Session{ID: id, ProjectID: f.project.ID, RepositoryID: f.repo.ID, TicketID: f.task.ID, Task: id,
		WorkingDir: ws.Path, WorkspaceID: ws.ID, Branch: ws.Branch, Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}
	return ws
}

// remoteRefs is every ref of the origin, as git lists them.
func (f *mergeFixture) remoteRefs(t *testing.T) string {
	t.Helper()
	return gitE2E(t, f.bare, "for-each-ref", "--format=%(refname) %(objectname)")
}

// snapshot is what a merge that refuses must leave alone: HEAD, the index's
// tree, the working tree's status and the content of the tracked files.
func (f *mergeFixture) snapshot(t *testing.T) string {
	t.Helper()
	files := gitE2E(t, f.checkout, "ls-files")
	var b strings.Builder
	for _, name := range strings.Fields(files) {
		body, _ := os.ReadFile(filepath.Join(f.checkout, name))
		b.WriteString(name + "=" + string(body) + "\n")
	}
	return gitE2E(t, f.checkout, "rev-parse", "HEAD") + "\n" + gitE2E(t, f.checkout, "write-tree") + "\n" +
		gitE2E(t, f.checkout, "status", "--porcelain=v2", "--branch") + "\n" + b.String()
}
