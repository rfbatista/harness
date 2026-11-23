package ports

import (
	"context"
	"time"
)

// The worktrees the New session form's picker offers for a task in a
// repository (contract "Harness server ↔ Web UI — joining a worktree and the
// task worktree setting", §1).

// TaskWorktreePolicy is "One branch and worktree per task" as it stands for a
// task: the project's setting, the task's override ("project", "on" or "off")
// and what is in effect.
type TaskWorktreePolicy struct {
	Project      bool   `json:"project"`
	TaskOverride string `json:"task_override"`
	Effective    bool   `json:"effective"`
}

// TaskWorktreeSession is one session on a worktree.
type TaskWorktreeSession struct {
	SessionID string `json:"session_id"`
	Agent     string `json:"agent"`
	Mode      string `json:"mode"`
	// Access is "write" or "read_only"; a session from before shared
	// worktrees is write.
	Access  string `json:"access"`
	Running bool   `json:"running"`
	// EndedAt is when the session ended, for "ended 2h ago"; nil while it runs.
	EndedAt *time.Time `json:"ended_at"`
}

// TaskWorktreeChoice is one worktree of the task.
type TaskWorktreeChoice struct {
	WorktreeID         string                `json:"worktree_id"`
	Branch             string                `json:"branch"`
	OnDisk             bool                  `json:"on_disk"`
	CreatedBySessionID string                `json:"created_by_session_id"`
	Sessions           []TaskWorktreeSession `json:"sessions"`
}

// TaskWorktreeChoices is the picker's list, newest worktree first.
type TaskWorktreeChoices struct {
	Policy         TaskWorktreePolicy   `json:"policy"`
	TaskWorktreeID *string              `json:"task_worktree_id"`
	ChoiceNeeded   bool                 `json:"choice_needed"`
	Worktrees      []TaskWorktreeChoice `json:"worktrees"`
}

// TaskWorktreeLister answers GET /api/task_worktrees (driving; orchestration
// implements it). Only worktrees on disk of sessions of the task in the
// repository are listed.
type TaskWorktreeLister interface {
	TaskWorktreeChoices(ctx context.Context, taskID, repositoryID string) (*TaskWorktreeChoices, error)
}
