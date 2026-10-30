package domain

import "time"

// The upstream states of a branch: how it stands against the remote branch
// it tracks, as of the repository's last fetch.
const (
	UpstreamNone     = "none"     // no upstream configured, or a detached HEAD
	UpstreamGone     = "gone"     // configured, but its remote-tracking ref no longer exists
	UpstreamUnknown  = "unknown"  // configured and present, but neither fetched nor pushed by this server
	UpstreamInSync   = "in_sync"  // ahead 0, behind 0
	UpstreamAhead    = "ahead"    // ahead only
	UpstreamBehind   = "behind"   // behind only
	UpstreamDiverged = "diverged" // both
)

// GitState is where a session worktree's branch stands: against the branch it
// was cut from, against its upstream, and what is uncommitted in it. It is
// read from local git only, never from the network.
type GitState struct {
	WorkspaceID  string `json:"workspace_id"`
	RepositoryID string `json:"repository_id"`
	SessionID    string `json:"session_id"`
	// Branch is "" when HEAD is detached; Head is "" before the first commit.
	Branch string `json:"branch"`
	Head   string `json:"head"`
	// Gone: the worktree is no longer on disk; the counts are the
	// repository's and Uncommitted is zero.
	Gone     bool          `json:"gone"`
	Base     BaseState     `json:"base"`
	Upstream UpstreamState `json:"upstream"`
	// FetchedAt is when the repository's origin was last fetched; nil when
	// never.
	FetchedAt *time.Time `json:"fetched_at"`
	// PushedAt is when the server last pushed the branch successfully; nil
	// when never. A push writes the remote-tracking ref, so it lifts unknown.
	PushedAt    *time.Time  `json:"pushed_at"`
	Uncommitted Uncommitted `json:"uncommitted"`
	ReadAt      time.Time   `json:"read_at"`
}

// BaseState compares the branch with the ref the workspace was cut from. Ref
// is the configured name even when it no longer resolves; Gone says so, with
// 0 and 0.
type BaseState struct {
	Ref    string `json:"ref"`
	Gone   bool   `json:"gone"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
}

// UpstreamState is the branch against its upstream. Ref, Remote, Ahead and
// Behind are nil for none; Ahead and Behind are also nil for gone and unknown.
type UpstreamState struct {
	State  string  `json:"state"`
	Ref    *string `json:"ref"`
	Remote *string `json:"remote"`
	Ahead  *int    `json:"ahead"`
	Behind *int    `json:"behind"`
}

// Uncommitted counts the files that differ from HEAD: tracked ones (on disk or
// in the index), untracked ones git does not ignore, and the tracked ones
// with a change in the index (part of Tracked).
type Uncommitted struct {
	Tracked   int `json:"tracked"`
	Untracked int `json:"untracked"`
	Staged    int `json:"staged"`
}

// Codes of fetching and pushing a worktree's branch.
const (
	CodeNoRemote     = "NO_REMOTE"     // the repository has no origin remote
	CodeFetchFailed  = "FETCH_FAILED"  // a fetch failed for a reason other than the credential
	CodePushRejected = "PUSH_REJECTED" // the remote refused: not a fast-forward, or a hook
	CodePushStale    = "PUSH_STALE"    // the branch's HEAD is not the one the person confirmed
	CodePushFailed   = "PUSH_FAILED"   // any other push failure
)

// GitStateError is a coded refusal that carries the worktree's state as it is
// now, so a client can show it without another read.
func GitStateError(code, message string, state *GitState) error {
	return &DetailedError{StructuredError: &StructuredError{Code: code, Message: message}, Details: map[string]any{"state": state}}
}
