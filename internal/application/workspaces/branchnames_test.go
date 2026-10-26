package workspaces_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
)

func newBranchNames(f *mergeFixture) *workspaces.BranchNames {
	return workspaces.NewBranchNames(sqlite.NewRepositoryRepository(f.db), sqlite.NewTicketRepository(f.db), gitcli.NewBranchNames())
}

func TestCheckBranchName(t *testing.T) {
	f := newMergeFixture(t, "main")
	gitRun(t, f.repoDir, "branch", "feat")
	gitRun(t, f.repoDir, "branch", "feat-2")
	svc := newBranchNames(f)
	refs := gitOut(t, f.repoDir, "for-each-ref", "refs/")

	for _, c := range []struct {
		name, trimmed    string
		valid, available bool
		code, suggestion string
		reasonHas        string
	}{
		{"", "", false, false, "", "", ""},
		{"  my/branch  ", "my/branch", true, true, "", "", ""},
		{"feat", "feat", true, false, domain.BranchExists, "feat-3", "feat"},
		{"main", "main", true, false, domain.BranchExists, "main-2", "main"},
		{"bad name", "bad name", false, false, domain.BranchNameInvalid, "", "spaces"},
		{"a..b", "a..b", false, false, domain.BranchNameInvalid, "", ".."},
	} {
		got, err := svc.CheckBranchName(context.Background(), f.repoID, c.name, "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != c.trimmed || got.Valid != c.valid || got.Available != c.available ||
			got.Code != c.code || got.Suggestion != c.suggestion || !strings.Contains(got.Reason, c.reasonHas) {
			t.Errorf("%q: %+v", c.name, got)
		}
		if !strings.HasPrefix(got.Generated, "agent/") {
			t.Errorf("%q: generated %q", c.name, got.Generated)
		}
	}
	if after := gitOut(t, f.repoDir, "for-each-ref", "refs/"); after != refs {
		t.Fatalf("a check changed refs:\n%s\n%s", refs, after)
	}
}

func TestCheckBranchNameGeneratesFromTheTask(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	got, err := newBranchNames(f).CheckBranchName(context.Background(), f.repoID, "", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Generated, "agent/task-") || got.Code != "" || got.Reason != "" {
		t.Fatalf("%+v", got)
	}
}

func TestCheckBranchNameOfAnUnknownRepository(t *testing.T) {
	f := newMergeFixture(t, "main")
	_, err := newBranchNames(f).CheckBranchName(context.Background(), "nope", "x", "")
	if errs.Code(err) != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
}

// A start never reuses an existing branch, whatever its state, and a refused
// start leaves no worktree, no workspace and no branch behind.
func TestCreateRefusesAnExistingBranchAndCreatesNothing(t *testing.T) {
	f := newMergeFixture(t, "main")
	gitRun(t, f.repoDir, "branch", "feat")
	settings := sqlite.NewSettingsRepository(f.db)
	root := t.TempDir()
	if err := settings.Set(domain.SettingWorkspacesRoot, root); err != nil {
		t.Fatal(err)
	}
	svc := workspaces.NewService(sqlite.NewWorkspaceRepository(f.db), sqlite.NewRepositoryRepository(f.db), gitcli.NewWorktreeManager(), settings)
	refs := gitOut(t, f.repoDir, "for-each-ref", "refs/")

	for _, branch := range []string{"feat", "main"} {
		if _, err := svc.Create(f.repoID, "x-"+branch, branch, "main"); errs.Code(err) != domain.BranchExists {
			t.Fatalf("Create on existing %q = %v, want BRANCH_EXISTS", branch, err)
		}
	}
	if got := gitOut(t, f.repoDir, "for-each-ref", "refs/"); got != refs {
		t.Fatalf("refs changed:\n%s\n%s", refs, got)
	}
	if left := svc.ListByRepository(f.repoID); len(left) != 0 {
		t.Fatalf("workspaces left: %v", left)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("worktree directories left: %v", entries)
	}
	if wts := gitOut(t, f.repoDir, "worktree", "list", "--porcelain"); strings.Count(wts, "worktree ") != 1 {
		t.Fatalf("worktrees: %s", wts)
	}

	ws, err := svc.Create(f.repoID, "fresh", "my/fresh", "main")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Branch != "my/fresh" || gitOut(t, ws.Path, "rev-parse", "--abbrev-ref", "HEAD") != "my/fresh" {
		t.Fatalf("workspace %+v", ws)
	}
}
