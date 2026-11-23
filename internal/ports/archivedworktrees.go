package ports

import "context"

// ArchivedWorktreesScope names what a clear covers: one archived task (a
// feature counts its children) or every archived task of a project. Exactly
// one of the two is set.
type ArchivedWorktreesScope struct {
	TaskID    string
	ProjectID string
}

// ClearSkip is a worktree a clear left in place: Reason is "running" (the
// session, or another session on the same worktree, runs), "shared" (a session
// of a task not in the clear is on the worktree), "error" (who is on the
// worktree could not be read, so it stays) or "failed", and Message says why
// only for the last two.
type ClearSkip struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"`
	Reason    string `json:"reason"`
	Message   string `json:"message,omitempty"`
}

// ClearResult is what a clear did; Skipped is never nil.
type ClearResult struct {
	Cleared int         `json:"cleared"`
	Skipped []ClearSkip `json:"skipped"`
}

// ArchivedWorktrees is the person's Clear worktrees action (ADR 0089). It
// removes the worktrees of archived tasks' sessions from disk, discarding
// uncommitted changes, and keeps every branch, session and transcript. A
// running session's worktree is skipped, and one failure does not stop the
// others. A task that is not archived is TASK_NOT_ARCHIVED.
type ArchivedWorktrees interface {
	// Preview counts what Clear would do, changing nothing: the worktrees it
	// would remove, how many of them hold uncommitted changes, and how many
	// belong to running sessions and would be skipped.
	Preview(ctx context.Context, scope ArchivedWorktreesScope) (*ArchivedWorktreesPreview, error)
	Clear(ctx context.Context, scope ArchivedWorktreesScope) (*ClearResult, error)
}

// ArchivedWorktreesPreview is what the Clear worktrees confirm shows.
type ArchivedWorktreesPreview struct {
	Worktrees int `json:"worktrees"`
	Dirty     int `json:"dirty"`
	Running   int `json:"running"`
	// Shared counts the sessions whose worktree stays because a session of a
	// task outside the clear is on it.
	Shared int `json:"shared"`
}
