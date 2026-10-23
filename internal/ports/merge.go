package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// BranchMerger merges a session's branch into its repository's default
// branch, in the repository's local checkout. The harness server, which runs
// unsandboxed, does it for the person (web UI) or for an architect (task
// tool): no agent needs write access to a main checkout. It never pushes,
// restarts nothing, and does not move the task's status.
type BranchMerger interface {
	// MergeSessionBranch merges with --no-ff. A refusal is a StructuredError
	// coded with one of domain.Merge*, and is recorded on the session like a
	// success: SESSION_NOT_FOUND is the only error that is not.
	MergeSessionBranch(ctx context.Context, sessionID string) (*domain.MergeResult, error)
	// MergeOffer says whether the session has a branch and a checkout a merge
	// could apply to, and into which branch it would merge.
	MergeOffer(ctx context.Context, sessionID string) (*MergeOffer, error)
}

// MergeOffer is whether a merge applies to a session.
type MergeOffer struct {
	Available bool `json:"available"`
	// Into is the default branch a merge goes into; empty when none is known.
	Into string `json:"into,omitempty"`
	// Reason is the domain.Merge code that says why it is not available.
	Reason string `json:"reason,omitempty"`
}

// CheckoutState is where a checkout is, read in one git run.
type CheckoutState struct {
	// Branch is the checked-out branch; empty on a detached HEAD.
	Branch string
	// Head is the commit checked out; empty in a repository with none.
	Head string
	// Tracked and Untracked are the paths with uncommitted changes to tracked
	// files and the paths of untracked files; ignored files are in neither.
	Tracked, Untracked []string
}

// Clean reports whether nothing is uncommitted.
func (s CheckoutState) Clean() bool { return len(s.Tracked) == 0 && len(s.Untracked) == 0 }

// GitMerger merges in a checkout (driven; gitcli implements it). Every run is
// bounded by a deadline.
type GitMerger interface {
	// HasBranch reports whether refs/heads/<branch> exists.
	HasBranch(ctx context.Context, dir, branch string) bool
	// Checkout reads dir's branch, HEAD and uncommitted paths. A directory
	// that is no git checkout is MERGE_NO_CHECKOUT.
	Checkout(ctx context.Context, dir string) (CheckoutState, error)
	// Merge merges branch into the branch checked out in dir with --no-ff and
	// answers the new HEAD. It refuses with MERGE_BRANCH_NOT_FOUND,
	// MERGE_ALREADY_MERGED (nothing changes) or MERGE_CONFLICT; a conflict,
	// like any failed merge, is aborted and checked to have left HEAD, index
	// and working tree as they were. It never pushes.
	Merge(ctx context.Context, dir, branch string) (head string, err error)
}

// SessionChangeAnnouncer puts a session's current record on its project's feed
// (the orchestration implements it), after something changed it outside the
// session's own events.
type SessionChangeAnnouncer interface {
	AnnounceSession(sessionID string)
}
