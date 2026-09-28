package ports

import "operators-mcp/internal/domain"

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
