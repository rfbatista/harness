package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// Driving ports of the workspaces service: git worktrees of a repository.
// *workspaces.Service satisfies them.

// WorkspaceManager lists, creates and removes a repository's workspaces.
type WorkspaceManager interface {
	Create(repositoryID, name, branch, baseRef string) (*domain.Workspace, error)
	Get(id string) *domain.Workspace
	ListByRepository(repositoryID string) []*domain.Workspace
	ListBranches(repositoryID string) ([]domain.GitBranch, error)
	// Delete removes the worktree and keeps its branch, so committed work
	// outlives the workspace.
	Delete(id string) error
}

// WorkspaceProvisioner is what session start needs: a worktree for a new
// session, and a way to take it back.
type WorkspaceProvisioner interface {
	Create(repositoryID, name, branch, baseRef string) (*domain.Workspace, error)
	Delete(id string) error
	// Discard is a failed start's rollback: unlike Delete it also removes the
	// branch, which is safe only because a just-provisioned workspace's branch
	// holds no commits.
	Discard(id string) error
}

// Workspaces is the whole workspace surface.
type Workspaces interface {
	WorkspaceManager
	WorkspaceProvisioner
}

// HistoryQuery picks what a repository's history read returns.
type HistoryQuery struct {
	// Limit caps the commits, newest first in topological order;
	// 0 means the default, at most domain.MaxHistory.
	Limit int
	// Remotes adds remote-tracking branches to the local ones and tags.
	Remotes bool
	// Refs, when set, logs only what these refs reach (a session's branch
	// and its base) instead of every branch and tag.
	Refs []string
}

// RepositoryHistory reads a repository's commit graph: every local branch
// (agents' session branches among them), tags, optionally remote branches.
// An unknown repository is REPOSITORY_NOT_FOUND; an unknown commit
// COMMIT_NOT_FOUND. *workspaces.Service satisfies it.
type RepositoryHistory interface {
	RepositoryLog(ctx context.Context, repositoryID string, q HistoryQuery) ([]domain.Commit, error)
	RepositoryCommit(ctx context.Context, repositoryID, hash string) (*domain.CommitDetail, error)
	// WorkspaceHistory is a workspace's (a session's worktree) branch beside
	// its base, up to limit commits, with its uncommitted changes; an unknown
	// workspace is WORKSPACE_NOT_FOUND.
	WorkspaceHistory(ctx context.Context, workspaceID string, limit int) (*domain.WorkspaceHistory, error)
}
