package workspaces_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const costFiles = 500

// countingGit counts the git reads whose number is the cost of a code read:
// the paths patches were read for, and the whole-worktree listings.
type countingGit struct {
	ports.GitCode
	patchCalls, patchPaths, listings atomic.Int64
}

func (g *countingGit) Patch(ctx context.Context, dir, base, head string, paths []string, maxLines int, maxBytes int64) (map[string]*domain.FileDiff, error) {
	g.patchCalls.Add(1)
	g.patchPaths.Add(int64(len(paths)))
	return g.GitCode.Patch(ctx, dir, base, head, paths, maxLines, maxBytes)
}

func (g *countingGit) WorktreeEntries(ctx context.Context, dir string, deleted bool) ([]ports.GitEntry, error) {
	g.listings.Add(1)
	return g.GitCode.WorktreeEntries(ctx, dir, deleted)
}

type costFixture struct {
	code        *workspaces.Code
	git         *countingGit
	workspaceID string
	worktree    string
	clock       *costClock
}

type costClock struct{ now time.Time }

func (c *costClock) Now() time.Time { return c.now }

// newCostFixture is a repository of costFiles files in ten directories and a
// session worktree that has modified every one of them.
func newCostFixture(t *testing.T) *costFixture {
	t.Helper()
	base := t.TempDir()
	repo, wt := filepath.Join(base, "repo"), filepath.Join(base, "wt")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Cost", "-c", "user.email=cost@example.com",
			"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(dir string, content string) {
		t.Helper()
		for i := range costFiles {
			p := filepath.Join(dir, fmt.Sprintf("d%d", i/50), fmt.Sprintf("f%03d.txt", i))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(repo, "init", "-q", "-b", "main")
	write(repo, "one\n")
	git(repo, "add", "-A")
	git(repo, "commit", "-q", "-m", "files")
	git(repo, "worktree", "add", "-q", "-b", "session", wt, "main")
	write(wt, "one\ntwo\n")

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	r, err := repos.Create("cost-project", "cost", "", "https://example.com/cost.git", repo)
	if err != nil {
		t.Fatal(err)
	}
	wsRepo := sqlite.NewWorkspaceRepository(db)
	ws, err := wsRepo.Create(domain.Workspace{RepositoryID: r.ID, Name: "session", Branch: "session", Path: wt, BaseRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	counting := &countingGit{GitCode: gitcli.NewCode()}
	clock := &costClock{now: time.Now()}
	code := workspaces.NewCode(workspaces.CodeDeps{
		Workspaces: wsRepo, Repositories: repos, Git: counting, Files: filesystem.NewWorktreeFiles(), Now: clock.Now,
	})
	return &costFixture{code: code, git: counting, workspaceID: ws.ID, worktree: wt, clock: clock}
}

func TestADiffPageReadsPatchesForItsOwnFilesOnly(t *testing.T) {
	f := newCostFixture(t)
	ctx := context.Background()

	d, err := f.code.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: f.workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != domain.DiffPageFiles || d.FileCount != costFiles || d.Added != costFiles {
		t.Fatalf("first page = %d files, count %d, +%d", len(d.Files), d.FileCount, d.Added)
	}
	if got := f.git.patchPaths.Load(); got != domain.DiffPageFiles {
		t.Errorf("patches read for %d paths, want the page's %d", got, domain.DiffPageFiles)
	}

	f.git.patchCalls.Store(0)
	totals, err := f.code.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: f.workspaceID, TotalsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if totals.FileCount != costFiles || len(totals.Files) != 0 {
		t.Errorf("totals read = %d files, count %d", len(totals.Files), totals.FileCount)
	}
	if got := f.git.patchCalls.Load(); got != 0 {
		t.Errorf("a totals-only read ran %d patch reads, want none", got)
	}
}

func TestReadingManyDirectoriesWithinTheWatchIntervalListsTheWorktreeOnce(t *testing.T) {
	f := newCostFixture(t)
	ctx := context.Background()
	src := ports.CodeSourceQuery{WorkspaceID: f.workspaceID}

	for i := range 10 {
		tree, err := f.code.CodeTree(ctx, src, fmt.Sprintf("d%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Entries) != 50 || tree.Entries[0].Change == nil || *tree.Entries[0].Change != domain.ChangeModified {
			t.Fatalf("d%d = %d entries, first change %v", i, len(tree.Entries), tree.Entries[0].Change)
		}
	}
	if got := f.git.listings.Load(); got != 1 {
		t.Errorf("ten directory reads listed the worktree %d times, want once", got)
	}

	f.clock.now = f.clock.now.Add(workspaces.CodeWatchInterval + time.Millisecond)
	if _, err := f.code.CodeTree(ctx, src, "d0"); err != nil {
		t.Fatal(err)
	}
	if got := f.git.listings.Load(); got != 2 {
		t.Errorf("a read after the interval listed the worktree %d times in all, want twice", got)
	}
}
