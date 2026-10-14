package sqlite

import (
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// UsageRecordModel is the GORM model for domain.UsageRecord. Records are
// kept forever; times are unix milliseconds.
type UsageRecordModel struct {
	ID                       string `gorm:"primaryKey"`
	Attribution              string `gorm:"column:attribution"`
	ProjectID                string `gorm:"column:project_id;index:idx_usage_project_started,priority:1"`
	TaskID                   string `gorm:"column:task_id;index"`
	SessionID                string `gorm:"column:session_id;index"`
	Method                   string `gorm:"column:method"`
	Path                     string `gorm:"column:path"`
	Model                    string `gorm:"column:model"`
	Status                   int    `gorm:"column:status"`
	Streamed                 bool   `gorm:"column:streamed"`
	StartedAt                int64  `gorm:"column:started_at;index:idx_usage_project_started,priority:2"`
	DurationMS               int64  `gorm:"column:duration_ms"`
	InputTokens              int64  `gorm:"column:input_tokens"`
	OutputTokens             int64  `gorm:"column:output_tokens"`
	CacheCreationInputTokens int64  `gorm:"column:cache_creation_input_tokens"`
	CacheReadInputTokens     int64  `gorm:"column:cache_read_input_tokens"`
	Error                    string `gorm:"column:error"`
}

func (UsageRecordModel) TableName() string { return "usage_records" }

func (m *UsageRecordModel) ToDomain() *domain.UsageRecord {
	return &domain.UsageRecord{
		ID: m.ID, Attribution: m.Attribution, ProjectID: m.ProjectID, TaskID: m.TaskID, SessionID: m.SessionID,
		Method: m.Method, Path: m.Path, Model: m.Model, Status: m.Status, Streamed: m.Streamed,
		StartedAt: time.UnixMilli(m.StartedAt), DurationMS: m.DurationMS,
		InputTokens: m.InputTokens, OutputTokens: m.OutputTokens,
		CacheCreationInputTokens: m.CacheCreationInputTokens, CacheReadInputTokens: m.CacheReadInputTokens,
		Error: m.Error,
	}
}

// UsageRepository implements ports.UsageRepository.
type UsageRepository struct{ db *gorm.DB }

var _ ports.UsageRepository = (*UsageRepository)(nil)

func NewUsageRepository(db *gorm.DB) *UsageRepository {
	return &UsageRepository{db: db}
}

// Create stores r in one insert; an empty ID gets a generated one.
func (r *UsageRepository) Create(u *domain.UsageRecord) error {
	id := u.ID
	if id == "" {
		var err error
		if id, err = genID(); err != nil {
			return err
		}
	}
	return r.db.Create(&UsageRecordModel{
		ID: id, Attribution: u.Attribution, ProjectID: u.ProjectID, TaskID: u.TaskID, SessionID: u.SessionID,
		Method: u.Method, Path: u.Path, Model: u.Model, Status: u.Status, Streamed: u.Streamed,
		StartedAt: u.StartedAt.UnixMilli(), DurationMS: u.DurationMS,
		InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens, CacheReadInputTokens: u.CacheReadInputTokens,
		Error: u.Error,
	}).Error
}

func (r *UsageRepository) ListRecent(f ports.UsageFilter) ([]*domain.UsageRecord, error) {
	var ms []UsageRecordModel
	if err := r.db.Where("project_id = ?", f.ProjectID).Order("started_at DESC, rowid DESC").Limit(f.Limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.UsageRecord, len(ms))
	for i := range ms {
		out[i] = ms[i].ToDomain()
	}
	return out, nil
}
