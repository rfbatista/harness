package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.PushRecords = (*RecordedPushRepository)(nil)

// RecordedPushModel is the GORM model of domain.RecordedPush.
type RecordedPushModel struct {
	ID           uint   `gorm:"primaryKey;autoIncrement"`
	RequestID    string `gorm:"column:request_id"`
	SessionID    string `gorm:"column:session_id;index"`
	RepositoryID string `gorm:"column:repository_id"`
	Branch       string
	Remote       string
	RemoteBranch string `gorm:"column:remote_branch"`
	Commit       string
	At           time.Time
	By           string
}

// TableName overrides the table name.
func (RecordedPushModel) TableName() string { return "recorded_pushes" }

// RecordedPushRepository persists the pushes the harness made.
type RecordedPushRepository struct{ db *gorm.DB }

// NewRecordedPushRepository returns a new recorded push repository.
func NewRecordedPushRepository(db *gorm.DB) *RecordedPushRepository {
	return &RecordedPushRepository{db: db}
}

// RecordPush implements ports.PushRecords.
func (r *RecordedPushRepository) RecordPush(ctx context.Context, p domain.RecordedPush) error {
	return r.db.WithContext(ctx).Create(&RecordedPushModel{
		RequestID: p.RequestID, SessionID: p.SessionID, RepositoryID: p.RepositoryID, Branch: p.Branch,
		Remote: p.Remote, RemoteBranch: p.RemoteBranch, Commit: p.Commit, At: p.At, By: p.By,
	}).Error
}

// PushesOf implements ports.PushRecords.
func (r *RecordedPushRepository) PushesOf(ctx context.Context, sessionID string) ([]domain.RecordedPush, error) {
	var rows []RecordedPushModel
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.RecordedPush, len(rows))
	for i, m := range rows {
		out[i] = domain.RecordedPush{
			RequestID: m.RequestID, SessionID: m.SessionID, RepositoryID: m.RepositoryID, Branch: m.Branch,
			Remote: m.Remote, RemoteBranch: m.RemoteBranch, Commit: m.Commit, At: m.At, By: m.By,
		}
	}
	return out, nil
}
