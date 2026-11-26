package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BenchScorecardStore = (*BenchScorecardRepository)(nil)

// BenchScorecardModel is the scorecard of one bench run of a project (ADR
// 0015), kept whole as JSON: it is read and written whole and compared with
// the one before it.
type BenchScorecardModel struct {
	ID        string `gorm:"primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	StartedAt time.Time
	Data      string
}

// TableName overrides the table name.
func (BenchScorecardModel) TableName() string { return "bench_scorecards" }

// BenchScorecardRepository stores the scorecards.
type BenchScorecardRepository struct{ db *gorm.DB }

// NewBenchScorecardRepository returns the store.
func NewBenchScorecardRepository(db *gorm.DB) *BenchScorecardRepository {
	return &BenchScorecardRepository{db: db}
}

// Save stores a scorecard.
func (r *BenchScorecardRepository) Save(ctx context.Context, card domain.Scorecard) error {
	data, err := json.Marshal(card)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Save(&BenchScorecardModel{ID: card.ID, ProjectID: card.ProjectID, StartedAt: card.StartedAt, Data: string(data)}).Error
}

// Get answers the project's scorecard, nil when there is none.
func (r *BenchScorecardRepository) Get(ctx context.Context, projectID, id string) (*domain.Scorecard, error) {
	var row BenchScorecardModel
	err := r.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	card, err := row.toDomain()
	return &card, err
}

// List answers the project's scorecards, newest first.
func (r *BenchScorecardRepository) List(ctx context.Context, projectID string) ([]domain.Scorecard, error) {
	var rows []BenchScorecardModel
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("started_at desc, id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Scorecard, 0, len(rows))
	for _, row := range rows {
		card, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	return out, nil
}

func (m BenchScorecardModel) toDomain() (domain.Scorecard, error) {
	var card domain.Scorecard
	return card, json.Unmarshal([]byte(m.Data), &card)
}
