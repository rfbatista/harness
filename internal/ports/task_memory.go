package ports

import (
	"context"
	"time"
)

// TaskMemoryRow is one stored file of a task's memory in a repository.
type TaskMemoryRow struct {
	File      string
	Content   string
	UpdatedAt time.Time
}

// TaskMemoryStore keeps the task memory files (driven, ADR 0013): per task
// and repository, plan.md and progress.md. Last write wins per file.
type TaskMemoryStore interface {
	ListTaskMemory(ctx context.Context, taskID, repositoryID string) ([]TaskMemoryRow, error)
	PutTaskMemory(ctx context.Context, taskID, repositoryID, file, content string) error
}

// TaskMemoryFile is one file of a task memory as the API answers it.
type TaskMemoryFile struct {
	Name      string     `json:"name"`
	Content   string     `json:"content"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// TaskMemoryView is a task's memory in one repository: both files, a file
// nobody wrote yet holding its starter.
type TaskMemoryView struct {
	TaskID       string           `json:"task_id"`
	RepositoryID string           `json:"repository_id"`
	Files        []TaskMemoryFile `json:"files"`
}

// TaskMemories is the task memory use cases (*taskmemory.Service). Errors:
// TICKET_NOT_FOUND, REPOSITORY_NOT_FOUND, INVALID_INPUT (a file that is not
// plan.md or progress.md).
type TaskMemories interface {
	GetTaskMemory(ctx context.Context, taskID, repositoryID string) (*TaskMemoryView, error)
	SaveTaskMemory(ctx context.Context, taskID, repositoryID, file, content string) (*TaskMemoryView, error)
}

// TaskMemoryChange says a task's memory file changed; ProjectID routes it and
// is not on the wire.
type TaskMemoryChange struct {
	TaskID       string `json:"task_id"`
	RepositoryID string `json:"repository_id"`
	File         string `json:"file"`
	ProjectID    string `json:"-"`
}

// TaskMemoryDirEnv names, in a session's environment, the folder holding its
// task memory files (plan.md and progress.md), outside the worktree.
const TaskMemoryDirEnv = "HARNESS_TASK_MEMORY_DIR"
