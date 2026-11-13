package orchestration

import (
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/domain"
)

func TestSandbox_AnArchitectReadsTheRepositorysWorktreesFolderAndWritesItsOwnWorktree(t *testing.T) {
	w := newSandboxWorld(t)
	w.ownTempDir()
	sess, p := w.start(t, "architect")
	folder := filepath.Join(w.wsRoot, domain.RepositoryWorktreesDirName(w.svc.catalog.Repositories.(*fakeResolver).repo))
	if !slices.Contains(p.Readable, folder) || slices.Contains(p.Writable, folder) {
		t.Errorf("worktrees folder %s: readable %v writable %v", folder, p.Readable, p.Writable)
	}
	if !slices.Contains(p.Writable, sess.WorkingDir) {
		t.Errorf("own worktree %s not writable: %v", sess.WorkingDir, p.Writable)
	}
	if !slices.Contains(p.Hidden, w.wsRoot) {
		t.Errorf("the workspaces root is no longer hidden: %v", p.Hidden)
	}
}

func TestSandbox_NoOtherModeGrantsTheWorktreesFolder(t *testing.T) {
	for _, mode := range []string{"implement", "design", "", "product"} {
		for _, readAll := range []bool{false, true} {
			w := newSandboxWorld(t)
			w.ownTempDir()
			_, p := w.startWithReadAll(t, mode, readAll)
			folder := filepath.Join(w.wsRoot, domain.RepositoryWorktreesDirName(w.svc.catalog.Repositories.(*fakeResolver).repo))
			if slices.Contains(p.Readable, folder) || slices.Contains(p.Writable, folder) {
				t.Errorf("mode %q (read all %v) is granted %s", mode, readAll, folder)
			}
		}
	}
}

func TestBuildSandboxPolicy_AnArchitectsWorktreesFolderIsReadOnlyAroundItsOwnWorktree(t *testing.T) {
	p := BuildSandboxPolicy(SandboxSession{
		Mode: domain.SessionModeArchitect, Worktree: "/ws/r-1/wt", WorktreesDir: "/ws/r-1",
		Repositories: []string{"/opt/r"}, WorkspacesRoot: "/ws",
	})
	if !slices.Contains(p.Readable, "/ws/r-1") || !slices.Contains(p.Writable, "/ws/r-1/wt") || slices.Contains(p.Writable, "/ws/r-1") {
		t.Errorf("readable %v writable %v", p.Readable, p.Writable)
	}
	if !slices.Equal(p.Hidden, []string{"/ws"}) {
		t.Errorf("hidden = %v", p.Hidden)
	}
}
