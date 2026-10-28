package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// WorktreeGit is a session worktree's git state (driving): where its branch
// stands against its base and its upstream, and what is uncommitted in it.
// *workspaces.WorktreeGit satisfies it. Reads never fetch, push or wait on a
// network.
//
// Errors: INVALID_INPUT, WORKSPACE_NOT_FOUND, REPOSITORY_NOT_FOUND,
// TASK_NOT_FOUND, UNAVAILABLE.
type WorktreeGit interface {
	// GitState is the state of one workspace; a gone worktree answers with
	// Gone set.
	GitState(ctx context.Context, workspaceID string) (*domain.GitState, error)
}

// GitWorktreeStatus is what one status pass over a worktree reads.
type GitWorktreeStatus struct {
	// Head is "" before the first commit; Branch is "" when detached.
	Head, Branch string
	// Upstream is the remote-tracking ref as git names it ("origin/foo"),
	// "" when none is configured; UpstreamGone says it does not exist.
	Upstream     string
	UpstreamGone bool
	// Ahead and Behind count against the upstream (zero when it is none or
	// gone).
	Ahead, Behind int
	// Tracked, Untracked and Staged count the uncommitted files.
	Tracked, Untracked, Staged int
}

// GitBranchUpstream is a branch's upstream as its repository holds it.
type GitBranchUpstream struct {
	Ref, Remote   string // "" when none is configured
	Gone          bool
	Ahead, Behind int
}

// GitWorktreeReader reads a branch's state out of git (driven; gitcli
// implements it). Nothing it runs writes to the repository or the network.
type GitWorktreeReader interface {
	// WorktreeStatus reads a worktree in one status pass; a directory that is
	// gone is WORKSPACE_MISSING.
	WorktreeStatus(ctx context.Context, dir string) (*GitWorktreeStatus, error)
	// BranchUpstream reads a local branch's upstream and counts in the
	// repository's main checkout, for a worktree that is gone.
	BranchUpstream(ctx context.Context, repoRoot, branch string) (GitBranchUpstream, error)
	// ResolveCommit is the full hash of the commit ref names, or REF_NOT_FOUND.
	ResolveCommit(ctx context.Context, dir, ref string) (string, error)
	// CurrentBranch is the branch checked out in dir, "" when detached.
	CurrentBranch(ctx context.Context, dir string) (string, error)
	// Divergence counts the commits branch has that base lacks (ahead) and
	// the other way round (behind); an error when either does not resolve.
	Divergence(ctx context.Context, dir, base, branch string) (ahead, behind int, err error)
	// FetchedAt is when origin was last fetched into the repository at
	// repoRoot; the zero time when never.
	FetchedAt(repoRoot string) time.Time
}
