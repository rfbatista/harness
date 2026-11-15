package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/ports"
)

var _ ports.InstructionDraftStore = (*InstructionDraftRepository)(nil)

// InstructionDraftModel is the GORM model of one file of a task's instruction
// draft, scoped to the task, not to a session.
type InstructionDraftModel struct {
	TaskID       string `gorm:"primaryKey;column:task_id"`
	RepositoryID string `gorm:"primaryKey;column:repository_id"`
	Path         string `gorm:"primaryKey;column:path"`
	Content      []byte
	UpdatedAt    time.Time
}

// TableName overrides the table name.
func (InstructionDraftModel) TableName() string { return "instruction_draft_files" }

// InstructionDraftRepository persists the instruction drafts.
type InstructionDraftRepository struct{ db *gorm.DB }

// NewInstructionDraftRepository returns a new draft store.
func NewInstructionDraftRepository(db *gorm.DB) *InstructionDraftRepository {
	return &InstructionDraftRepository{db: db}
}

// ListInstructionDrafts implements ports.InstructionDraftStore.
func (r *InstructionDraftRepository) ListInstructionDrafts(ctx context.Context, taskID string) ([]ports.InstructionDraftRow, error) {
	var rows []InstructionDraftModel
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("repository_id, path").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ports.InstructionDraftRow, len(rows))
	for i, m := range rows {
		out[i] = ports.InstructionDraftRow{RepositoryID: m.RepositoryID, Path: m.Path, Content: m.Content}
	}
	return out, nil
}

// PutInstructionDraft implements ports.InstructionDraftStore.
func (r *InstructionDraftRepository) PutInstructionDraft(ctx context.Context, taskID string, row ports.InstructionDraftRow) error {
	m := InstructionDraftModel{TaskID: taskID, RepositoryID: row.RepositoryID, Path: row.Path, Content: row.Content, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&m).Error
}

// DeleteInstructionDraft implements ports.InstructionDraftStore.
func (r *InstructionDraftRepository) DeleteInstructionDraft(ctx context.Context, taskID, repositoryID, path string) error {
	return r.db.WithContext(ctx).Where("task_id = ? AND repository_id = ? AND path = ?", taskID, repositoryID, path).Delete(&InstructionDraftModel{}).Error
}
