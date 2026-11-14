package ports

import "context"

// ArchivedWorktreesScope names what a clear covers: one archived task (a
// feature counts its children) or every archived task of a project. Exactly
// one of the two is set.
type ArchivedWorktreesScope struct {
	TaskID    string
	ProjectID string
}

// ClearSkip is a worktree a clear left in place: Reason is "running" or
// "failed", and Message says why only for a failure.
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
	Clear(ctx context.Context, scope ArchivedWorktreesScope) (*ClearResult, error)
}
