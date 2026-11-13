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
}

// TaskWorktrees answers which worktrees belong to a session's task (driving).
// It reads the session store alone, with no git call: hooks ask it on every
// decision (ADR 0033).
type TaskWorktrees interface {
	// TaskWorktrees is SESSION_NOT_FOUND for an unknown session; a session with
	// no task has no worktree beside the root.
	TaskWorktrees(ctx context.Context, sessionID string) (*TaskWorktreeList, error)
}
