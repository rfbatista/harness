package ports

import (
	"context"
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

// GitExcludes keeps paths out of git through a repository's info/exclude
// (driven; gitcli implements it).
type GitExcludes interface {
	// Exclude adds rel, anchored at the top of the repository at root, to
	// its info/exclude under a comment saying what it is, unless git
	// already ignores it. A directory's rel ends in "/".
	Exclude(root, rel, what string) error
}

// GitHistory reads a checkout's history (driven; gitcli implements it).
type GitHistory interface {
	// Log returns up to q.Limit commits reachable from the local branches,
	// tags and HEAD (and remote branches with q.Remotes), children before
	// their parents.
	Log(ctx context.Context, repoRoot string, q HistoryQuery) ([]domain.Commit, error)
	// Show returns one commit with its message and changed files, or
	// COMMIT_NOT_FOUND.
	Show(ctx context.Context, repoRoot, hash string) (*domain.CommitDetail, error)
	// Status reads a checkout's HEAD, branch and uncommitted changes; a
	// directory that is gone is WORKSPACE_MISSING.
	Status(ctx context.Context, dir string) (*domain.WorktreeStatus, error)
	// Divergence counts the commits branch has that base lacks (ahead) and
	// the other way round (behind).
	Divergence(ctx context.Context, dir, base, branch string) (ahead, behind int, err error)
}

// BranchMergeReader reads, in a repository's main checkout, how a set of its
// branches stand against the repository's default branch.
type BranchMergeReader interface {
	// BranchMerges answers for each of branches (by name), and names the
	// default branch it compared them with: "" when the repository has none.
	BranchMerges(ctx context.Context, repoRoot string, branches []BranchBase) (BranchMerges, error)
}

// BranchBase is a branch and the ref it was cut from ("" for HEAD).
type BranchBase struct{ Branch, Base string }

// BranchMerges is the answer of BranchMergeReader.BranchMerges.
type BranchMerges struct {
	DefaultBranch string
	Branches      map[string]BranchMerge
}

// BranchMerge is how one branch stands: whether it still exists, whether it
// carries commits of its own since it was cut, and whether the local default
// branch contains its tip.
type BranchMerge struct {
	Exists, Ahead, Merged bool
}
