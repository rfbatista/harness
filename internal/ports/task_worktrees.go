package ports

import "context"

// TaskWorktree is a session worktree of a task, as an architect's worktree
// read hook learns it (contract "Harness server ↔ Agent sessions — architect
// worktree visibility", "The lookup").
type TaskWorktree struct {
	SessionID string `json:"session_id"`
	Dir       string `json:"dir"`
	Branch    string `json:"branch"`
	Agent     string `json:"agent"`
	Status    string `json:"status"`
}

// TaskWorktreeList is the worktrees of a session's task in the session's
// repository, and the folder the harness makes every session worktree of that
// repository in.
type TaskWorktreeList struct {
	Root      string         `json:"worktrees_root"`
	Worktrees []TaskWorktree `json:"worktrees"`
	// Owner, when the lookup was asked about a path, is the session and task
	// of the worktree it lies in if that worktree belongs to another task: what
	// the hook's denial names. Absent otherwise.
	Owner *TaskWorktreeOwner `json:"owner,omitempty"`
}

// TaskWorktreeOwner is the session and task of a worktree that is not the
// caller's task's.
type TaskWorktreeOwner struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"`
}

// TaskWorktrees answers which worktrees belong to a session's task (driving).
// It reads the session store alone, with no git call: hooks ask it on every
// decision (ADR 0033).
type TaskWorktrees interface {
	// TaskWorktrees is SESSION_NOT_FOUND for an unknown session; a session with
	// no task has no worktree beside the root. A non-empty path adds the Owner
	// of the worktree it lies in when that is another task's.
	TaskWorktrees(ctx context.Context, sessionID, path string) (*TaskWorktreeList, error)
}

// WorktreeReadRecorder records a denial of the worktree read hook with the
// session, like a rule outcome (ADR 0033; driving).
type WorktreeReadRecorder interface {
	RecordWorktreeRead(ctx context.Context, sessionID, tool, path, reason string) error
}
