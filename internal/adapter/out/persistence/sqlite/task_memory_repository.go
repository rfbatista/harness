package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/ports"
)

var _ ports.TaskMemoryStore = (*TaskMemoryRepository)(nil)

// TaskMemoryModel is the GORM model of one file of a task's memory in a
// repository (ADR 0013).
type TaskMemoryModel struct {
	TaskID       string `gorm:"primaryKey;column:task_id"`
	RepositoryID string `gorm:"primaryKey;column:repository_id"`
	File         string `gorm:"primaryKey;column:file"`
	Content      string
	UpdatedAt    time.Time
}

// TableName overrides the table name.
func (TaskMemoryModel) TableName() string { return "task_memory_files" }

// TaskMemoryRepository persists the task memory files.
type TaskMemoryRepository struct{ db *gorm.DB }

// NewTaskMemoryRepository returns a new task memory store.
func NewTaskMemoryRepository(db *gorm.DB) *TaskMemoryRepository { return &TaskMemoryRepository{db: db} }

// ListTaskMemory implements ports.TaskMemoryStore.
func (r *TaskMemoryRepository) ListTaskMemory(ctx context.Context, taskID, repositoryID string) ([]ports.TaskMemoryRow, error) {
	var rows []TaskMemoryModel
	if err := r.db.WithContext(ctx).Where("task_id = ? AND repository_id = ?", taskID, repositoryID).Order("file").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ports.TaskMemoryRow, len(rows))
	for i, m := range rows {
		out[i] = ports.TaskMemoryRow{File: m.File, Content: m.Content, UpdatedAt: m.UpdatedAt}
	}
	return out, nil
}

// PutTaskMemory implements ports.TaskMemoryStore.
func (r *TaskMemoryRepository) PutTaskMemory(ctx context.Context, taskID, repositoryID, file, content string) error {
	m := TaskMemoryModel{TaskID: taskID, RepositoryID: repositoryID, File: file, Content: content, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&m).Error
}
