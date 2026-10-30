package vfs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeTickets and fakeDocuments give a project-level resources root nothing
// to hold but its README.
type fakeTickets struct{ ports.TicketReader }

func (fakeTickets) ListTickets(context.Context, string, ports.TicketFilter) ([]*domain.Ticket, error) {
	return nil, nil
}

type fakeDocuments struct{ ports.DocumentReader }

func (fakeDocuments) ListDocuments(string, domain.DocumentScope) []*domain.Document { return nil }

type fakeSessions struct {
	ports.SessionRepository
	list []*domain.Session
}

func (f fakeSessions) Get(id string) *domain.Session {
	for _, s := range f.list {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (f fakeSessions) List(context.Context, ports.SessionFilter) []*domain.Session { return f.list }

type fakeWorkspaces struct {
	ports.WorkspaceRepository
	byID map[string]*domain.Workspace
}

func (f fakeWorkspaces) Get(_ context.Context, id string) *domain.Workspace { return f.byID[id] }

// world is one session "s1" running in workspace "w1", on a real worktree.
type world struct {
	fs        *vfs.FileSystem
	resources *vfs.Service
	worktree  string
	events    *vfs.RootEvents
}

func newWorld(t *testing.T) *world {
	t.Helper()
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "internal", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	events := vfs.NewRootEvents()
	resources := vfs.New(vfs.Sources{Tickets: fakeTickets{}, Documents: fakeDocuments{}})
	resources.UseRootEvents(events)
	sessions := fakeSessions{list: []*domain.Session{{ID: "s1", WorkspaceID: "w1"}, {ID: "s2", WorkspaceID: "w1"}, {ID: "other", WorkspaceID: "w2"}, {ID: "bare"}}}
	workspaces := fakeWorkspaces{byID: map[string]*domain.Workspace{"w1": {ID: "w1", Path: worktree}}}
	return &world{
		fs:        vfs.NewFileSystem(resources, sessions, workspaces, filesystem.NewWorktreeFiles(), events),
		resources: resources, worktree: worktree, events: events,
	}
}

func (w *world) open(t *testing.T, session string) string {
	t.Helper()
	dir, err := w.resources.Open(context.Background(), session, ports.ShellScope{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.resources.Close(session, dir) })
	return dir
}

func rootOf(roots []vfs.Root, kind vfs.RootKind) vfs.Root {
	for _, r := range roots {
		if r.Kind == kind {
			return r
		}
	}
	return vfs.Root{}
}

func TestTheResourcesRootIsAvailableWhileTheSessionRunsAndNotAfter(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if r := rootOf(w.fs.Roots(ctx, "s1"), vfs.ResourcesRoot); r.Available || r.Kind == "" {
		t.Fatalf("before the session starts: %+v", r)
	}
	dir := w.open(t, "s1")
	r := rootOf(w.fs.Roots(ctx, "s1"), vfs.ResourcesRoot)
	if !r.Available || r.Path == "" {
		t.Fatalf("while it runs: %+v", r)
	}
	if real, _ := filepath.EvalSymlinks(dir); r.Path != real {
		t.Fatalf("Path = %q, want %q", r.Path, real)
	}
	w.resources.Close("s1", dir)
	if r := rootOf(w.fs.Roots(ctx, "s1"), vfs.ResourcesRoot); r.Available {
		t.Fatalf("after it ends: %+v", r)
	}
}

func TestTheWorktreeRootIsAvailableWhileItsDirectoryIsOnDisk(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if r := rootOf(w.fs.Roots(ctx, "s1"), vfs.WorktreeRoot); !r.Available || r.Path != w.worktree {
		t.Fatalf("worktree root = %+v", r)
	}
	if r := rootOf(w.fs.Roots(ctx, "bare"), vfs.WorktreeRoot); r.Available {
		t.Fatalf("a session with no workspace has a worktree root: %+v", r)
	}
	if err := os.RemoveAll(w.worktree); err != nil {
		t.Fatal(err)
	}
	if r := rootOf(w.fs.Roots(ctx, "s1"), vfs.WorktreeRoot); r.Available {
		t.Fatalf("after its directory is gone: %+v", r)
	}
}

func TestARealPathStaysInsideItsRoot(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(w.worktree, "escape")); err != nil {
		t.Fatal(err)
	}
	got, err := w.fs.RealPath(ctx, "s1", vfs.WorktreeRoot, "internal/app")
	if err != nil || got != filepath.Join(w.worktree, "internal", "app") {
		t.Fatalf("RealPath = %q, %v", got, err)
	}
	if got, err := w.fs.RealPath(ctx, "s1", vfs.WorktreeRoot, ""); err != nil || got != w.worktree {
		t.Fatalf("the root itself: %q, %v", got, err)
	}
	for _, folder := range []string{"..", "internal/../..", "/etc", "internal//app", "internal/app/", "main.go", "nope", "escape", "escape/x"} {
		if got, err := w.fs.RealPath(ctx, "s1", vfs.WorktreeRoot, folder); err == nil {
			t.Errorf("RealPath(%q) = %q, want a refusal", folder, got)
		}
	}
}

func TestARealPathOfAnUnavailableRootIsRefused(t *testing.T) {
	w := newWorld(t)
	if _, err := w.fs.RealPath(context.Background(), "s1", vfs.ResourcesRoot, ""); err == nil {
		t.Fatal("a resources root that is not open has a real path")
	}
	dir := w.open(t, "s1")
	real, _ := filepath.EvalSymlinks(dir)
	if got, err := w.fs.RealPath(context.Background(), "s1", vfs.ResourcesRoot, "task"); err == nil {
		t.Fatalf("a project-level root has no task/ folder: %q", got)
	}
	if got, err := w.fs.RealPath(context.Background(), "s1", vfs.ResourcesRoot, "project"); err != nil || got != filepath.Join(real, "project") {
		t.Fatalf("project/ = %q, %v", got, err)
	}
}
