package workspacestest

import (
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports/portstest"
)

// NewWorktreeGit records the fixture's repository and one workspace per branch
// in a fresh database and serves them through workspaces.WorktreeGit over real
// git and the real filesystem.
func NewWorktreeGit(t *testing.T, f *portstest.WorktreeGitFixture) (*workspaces.WorktreeGit, portstest.WorktreeGitTarget) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create("git-project", "fixture", "", f.OriginDir, f.RepoDir)
	if err != nil {
		t.Fatal(err)
	}
	wsRepo := sqlite.NewWorkspaceRepository(db)
	at := portstest.WorktreeGitTarget{RepositoryID: repo.ID, Workspaces: map[string]string{}}
	for _, branch := range portstest.WorktreeGitBranches {
		ws, err := wsRepo.Create(domain.Workspace{RepositoryID: repo.ID, Name: branch, Branch: branch, Path: f.Worktrees[branch], BaseRef: f.BaseRef})
		if err != nil {
			t.Fatal(err)
		}
		at.Workspaces[branch] = ws.ID
	}
	clock := &passingClock{now: time.Now()}
	svc := workspaces.NewWorktreeGit(workspaces.WorktreeGitDeps{
		Workspaces: wsRepo, Repositories: repos, Sessions: sqlite.NewSessionRepository(db),
		Git: gitcli.NewCode(), Files: filesystem.NewWorktreeFiles(), Now: clock.Now,
	})
	at.PassInterval = clock.Pass
	return svc, at
}
