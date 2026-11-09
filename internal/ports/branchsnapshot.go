package ports

import (
	"context"
	"time"
)

// GitRemote is a remote of a repository as configured.
type GitRemote struct {
	Name, URL string
}

// GitWorktreeHead is one worktree of a repository and what it has checked
// out; Branch is "" when HEAD is detached.
type GitWorktreeHead struct {
	Dir, Branch, Head string
}

// GitBranchSnapshot is what a repository holds of branches and remotes at one
// moment (ADR 0070): local branches and remote-tracking refs by name with their
// heads, the remotes, and what each worktree has checked out.
type GitBranchSnapshot struct {
	// Branches are the local branches by name; RemoteRefs the remote-tracking
	// refs by "remote/branch".
	Branches   map[string]string
	RemoteRefs map[string]string
	Remotes    []GitRemote
	Worktrees  []GitWorktreeHead
}

// GitReflogEntry is one line of a reflog.
type GitReflogEntry struct {
	Old, New string
	At       time.Time
	Message  string
}

// GitRemoteRefUpdate is one move of a remote-tracking ref: a push git itself
// recorded ("update by push") or a fetch.
type GitRemoteRefUpdate struct {
	Remote, Branch string
	Old, New       string
	At             time.Time
	Push           bool
}

// GitBranchEvidence reads a repository's branches and the evidence ADR 0070
// names (driven; gitcli implements it). Nothing it runs writes to the
// repository or the network, and its cost does not grow with the branches.
type GitBranchEvidence interface {
	// BranchSnapshot reads the branches, remotes and worktrees of the
	// repository at repoRoot.
	BranchSnapshot(ctx context.Context, repoRoot string) (*GitBranchSnapshot, error)
	// HEADReflog is the HEAD reflog of the worktree at dir, newest first.
	HEADReflog(ctx context.Context, dir string) ([]GitReflogEntry, error)
	// RemoteRefUpdates are the moves of every remote-tracking ref of the
	// repository at repoRoot, newest first.
	RemoteRefUpdates(ctx context.Context, repoRoot string) ([]GitRemoteRefUpdate, error)
}
