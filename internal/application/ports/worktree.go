package ports

import (
	"errors"

	"operators-mcp/internal/domain"
)

// ErrBranchExists is returned by WorktreeManager.Add when the branch it would
// create already exists. A session must never land on a branch another worktree
// may already hold, so the caller surfaces this instead of checking it out.
var ErrBranchExists = errors.New("branch already exists")

// WorktreeManager is the outbound port for materializing and removing git
// worktrees on disk. repoRoot is the repository's main checkout; path is the
// worktree directory; branch is created at baseRef (HEAD when baseRef is empty).
type WorktreeManager interface {
	Add(repoRoot, path, branch, baseRef string) error
	Remove(repoRoot, path string) error
	// ListBranches returns local branches first, then remote-tracking ones,
	// each group alphabetically. Symbolic */HEAD aliases are excluded.
	ListBranches(repoRoot string) ([]domain.GitBranch, error)
	// DeleteBranch force-deletes a local branch. It exists only for spawn
	// rollback (workspaces.Service.Discard), where the branch is provably a
	// few seconds old with no commits — never for session deletion, which
	// keeps the branch so committed work outlives the session.
	DeleteBranch(repoRoot, branch string) error
}
