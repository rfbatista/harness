// Package workspacestest wires the workspaces use cases over real adapters
// for the suites that run them on either side of the wire.
package workspacestest

import (
	"testing"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports/portstest"
)

// NewCode records the fixture's repository and its two workspaces in a
// fresh database and serves them through workspaces.Code over real git and
// the real filesystem.
func NewCode(t *testing.T, f *portstest.CodeFixture) (*workspaces.Code, portstest.CodeTarget) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create(f.ProjectID, "fixture", "", "https://example.com/fixture.git", f.RepoDir)
	if err != nil {
		t.Fatal(err)
	}
	wsRepo := sqlite.NewWorkspaceRepository(db)
	ws, err := wsRepo.Create(domain.Workspace{RepositoryID: repo.ID, Name: "session", Branch: f.Branch, Path: f.Worktree, BaseRef: f.BaseRef})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := wsRepo.Create(domain.Workspace{RepositoryID: repo.ID, Name: "gone", Branch: f.GoneBranch, Path: f.GoneWorktree, BaseRef: f.BaseRef})
	if err != nil {
		t.Fatal(err)
	}
	code := workspaces.NewCode(workspaces.CodeDeps{
		Workspaces: wsRepo, Repositories: repos, Git: gitcli.NewCode(), Files: filesystem.NewWorktreeFiles(),
		Architecture: f.Architecture(),
	})
	return code, portstest.CodeTarget{RepositoryID: repo.ID, WorkspaceID: ws.ID, GoneWorkspaceID: gone.ID}
}
