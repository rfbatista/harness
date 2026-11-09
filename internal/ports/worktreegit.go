package ports

import (
	"context"
	"slices"
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
	// TaskGitStates is the state of the workspace of every session of the
	// task that has one, in the task page's session order (newest session
	// first); a gone worktree is included, marked Gone.
	TaskGitStates(ctx context.Context, taskID string) ([]domain.GitState, error)
	// FetchRemote fetches the repository's origin (git fetch origin --prune)
	// with the stored credential, one at a time per repository and bounded in
	// time, and answers when it was fetched. Every workspace's state of the
	// repository is announced on the project feed after it. It is the fetch
	// the pull request base refresh runs.
	//
	// Errors: INVALID_INPUT, REPOSITORY_NOT_FOUND, NO_REMOTE,
	// GIT_CREDENTIAL_MISSING, GIT_CREDENTIAL_REJECTED,
	// GIT_CREDENTIAL_STORE_LOCKED, FETCH_FAILED.
	FetchRemote(ctx context.Context, repositoryID string) (time.Time, error)
	// PushBranch pushes the workspace's branch to origin with the stored
	// credential, setting its upstream, never forced, and answers the state
	// after it. expectedHead is the head the person confirmed: another HEAD
	// now is PUSH_STALE and nothing is pushed. One push per workspace runs at
	// a time and each is bounded in time. The worktree is not touched.
	//
	// Errors: INVALID_INPUT, WORKSPACE_NOT_FOUND, REPOSITORY_NOT_FOUND,
	// WORKSPACE_MISSING, NO_REMOTE, GIT_CREDENTIAL_MISSING,
	// GIT_CREDENTIAL_REJECTED, GIT_CREDENTIAL_STORE_LOCKED, PUSH_STALE and
	// PUSH_REJECTED (both with the state in their details), PUSH_FAILED.
	PushBranch(ctx context.Context, workspaceID, expectedHead string) (*domain.GitState, error)
	// PushBranchTo is PushBranch for a named remote and remote branch: the one
	// push path the person's Push, a harness push for an approved branch
	// request and the push_branch tool share (ADR 0071). The defaults are
	// PushBranch's. A branch other than the workspace's is pushed from the
	// repository, and ExpectedHead is then that branch's head.
	//
	// Errors: PushBranch's, and BRANCH_NOT_FOUND and REMOTE_NOT_FOUND.
	PushBranchTo(ctx context.Context, t PushTarget) (*domain.GitState, error)
}

// PushTarget names what one push sends where. Branch defaults to the
// workspace's branch, Remote to "origin", RemoteBranch to Branch.
type PushTarget struct {
	WorkspaceID  string
	ExpectedHead string
	Branch       string
	Remote       string
	RemoteBranch string
	// Commit pins what is pushed: exactly this commit, as the remote branch,
	// whatever the branch's head is now. "" pushes the branch's head.
	Commit string
	// RequestID is the branch request the push was approved under, "" for the
	// person's own Push; it only labels the record of the push.
	RequestID string
	// By says who pushed: "person" (default) or "agent".
	By string
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
	// StagedPaths are the tracked files with a change in the index (a renamed
	// one by its new path), in git's order.
	StagedPaths []string
}

// Equal says two status passes read the same.
func (s *GitWorktreeStatus) Equal(o *GitWorktreeStatus) bool {
	return s.Head == o.Head && s.Branch == o.Branch && s.Upstream == o.Upstream && s.UpstreamGone == o.UpstreamGone &&
		s.Ahead == o.Ahead && s.Behind == o.Behind && s.Tracked == o.Tracked && s.Untracked == o.Untracked &&
		s.Staged == o.Staged && slices.Equal(s.StagedPaths, o.StagedPaths)
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

// GitStatusSnapshotter is GitCode.Snapshot with the status pass it read, so
// the code watcher's one git run also gives the worktree's git state (driven;
// gitcli implements it).
type GitStatusSnapshotter interface {
	SnapshotStatus(ctx context.Context, dir string) (head, fingerprint string, st *GitWorktreeStatus, err error)
}
