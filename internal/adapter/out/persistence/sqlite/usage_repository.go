package sqlite

import (
	"context"
	"fmt"
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
func (r *UsageRepository) Create(ctx context.Context, u *domain.UsageRecord) error {
	id := u.ID
	if id == "" {
		var err error
		if id, err = genID(); err != nil {
			return err
		}
	}
	return r.db.WithContext(ctx).Create(&UsageRecordModel{
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

const usageSums = "COUNT(*) AS requests, SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens, " +
	"SUM(cache_creation_input_tokens) AS cache_creation_input_tokens, SUM(cache_read_input_tokens) AS cache_read_input_tokens"

// UsageSumsRow is what usageSums selects; exported so GORM scans it when
// embedded.
type UsageSumsRow struct {
	Requests                 int64
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

func (s UsageSumsRow) sums() ports.UsageSums {
	return ports.UsageSums{Requests: s.Requests, InputTokens: s.InputTokens, OutputTokens: s.OutputTokens,
		CacheCreationInputTokens: s.CacheCreationInputTokens, CacheReadInputTokens: s.CacheReadInputTokens}
}

func (r *UsageRepository) Groups(f ports.UsageGroupFilter) ([]ports.UsageGroup, error) {
	q := r.db.Model(&UsageRecordModel{})
	switch {
	case f.TaskID != "" && len(f.SessionIDs) > 0:
		q = q.Where("task_id = ? OR session_id IN ?", f.TaskID, f.SessionIDs)
	case f.TaskID != "":
		q = q.Where("task_id = ?", f.TaskID)
	case f.SessionID != "":
		q = q.Where("session_id = ?", f.SessionID)
	default:
		q = q.Where("project_id = ? AND started_at >= ?", f.ProjectID, f.Since.UnixMilli())
	}
	var rows []struct {
		TaskID    string
		SessionID string
		Model     string
		Long      bool
		UsageSumsRow
	}
	long := "(input_tokens + cache_creation_input_tokens + cache_read_input_tokens) > ?"
	err := q.Select("task_id, session_id, model, "+long+" AS long, "+usageSums, f.LongAbove).
		Group("task_id, session_id, model, long").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ports.UsageGroup, len(rows))
	for i, row := range rows {
		out[i] = ports.UsageGroup{TaskID: row.TaskID, SessionID: row.SessionID, Model: row.Model, Long: row.Long, UsageSums: row.sums()}
	}
	return out, nil
}

// bucketFormats are the local-time strftime formats a bucket's start is
// read with; SQLite's localtime is the process's zone, as time.Local is.
var bucketFormats = map[ports.UsageBucket]string{
	ports.UsageBucketHour: "%Y-%m-%d %H:00:00",
	ports.UsageBucketDay:  "%Y-%m-%d 00:00:00",
}

func (r *UsageRepository) Series(projectID string, since time.Time, bucket ports.UsageBucket) ([]ports.UsageSeriesPoint, error) {
	format, ok := bucketFormats[bucket]
	if !ok {
		return nil, fmt.Errorf("usage series: unknown bucket %q", bucket)
	}
	var rows []struct {
		Start string
		Model string
		UsageSumsRow
	}
	err := r.db.Model(&UsageRecordModel{}).
		Select("strftime(?, started_at / 1000, 'unixepoch', 'localtime') AS start, model, "+usageSums, format).
		Where("project_id = ? AND started_at >= ?", projectID, since.UnixMilli()).
		Group("start, model").Order("start, model").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ports.UsageSeriesPoint, len(rows))
	for i, row := range rows {
		start, err := time.ParseInLocation(time.DateTime, row.Start, time.Local)
		if err != nil {
			return nil, fmt.Errorf("usage series bucket %q: %w", row.Start, err)
		}
		out[i] = ports.UsageSeriesPoint{Start: start, Model: row.Model, UsageSums: row.sums()}
	}
	return out, nil
}
