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

var _ ports.ProductOwnerDigestStore = (*ProductOwnerDigestRepository)(nil)

// ProductOwnerDigestModel is the GORM model of a digest. Items hold the
// collected items as JSON.
type ProductOwnerDigestModel struct {
	ID          string `gorm:"primaryKey"`
	ProjectID   string `gorm:"column:project_id;index:idx_po_digests_project_date"`
	Date        string `gorm:"index:idx_po_digests_project_date"`
	PeriodStart time.Time
	PeriodEnd   time.Time
	Summary     string
	Items       string
}

// TableName overrides the table name.
func (ProductOwnerDigestModel) TableName() string { return "product_owner_digests" }

// ProductOwnerDigestStateModel is where a project's digests stand.
type ProductOwnerDigestStateModel struct {
	ProjectID   string `gorm:"primaryKey;column:project_id"`
	SinceAt     time.Time
	CheckedDate string `gorm:"column:checked_date"`
}

// TableName overrides the table name.
func (ProductOwnerDigestStateModel) TableName() string { return "product_owner_digest_states" }

// ProductOwnerDigestRepository persists digests in SQLite via GORM.
type ProductOwnerDigestRepository struct{ db *gorm.DB }

// NewProductOwnerDigestRepository returns a new digest repository.
func NewProductOwnerDigestRepository(db *gorm.DB) *ProductOwnerDigestRepository {
	return &ProductOwnerDigestRepository{db: db}
}

// SaveDigest implements ports.ProductOwnerDigestStore.
func (r *ProductOwnerDigestRepository) SaveDigest(ctx context.Context, d domain.ProductOwnerDigest) error {
	items, err := json.Marshal(d.Items)
	if err != nil {
		return err
	}
	m := ProductOwnerDigestModel{ID: d.ID, ProjectID: d.ProjectID, Date: d.Date, PeriodStart: d.PeriodStart, PeriodEnd: d.PeriodEnd, Summary: d.Summary, Items: string(items)}
	return r.db.WithContext(ctx).Save(&m).Error
}

// Digests implements ports.ProductOwnerDigestStore.
func (r *ProductOwnerDigestRepository) Digests(ctx context.Context, projectID, before string, limit int) ([]domain.ProductOwnerDigest, bool, error) {
	q := r.db.WithContext(ctx).Where("project_id = ?", projectID)
	if before != "" {
		q = q.Where("date < ?", before)
	}
	var models []ProductOwnerDigestModel
	if err := q.Order("date DESC, period_end DESC").Limit(limit + 1).Find(&models).Error; err != nil {
		return nil, false, err
	}
	more := len(models) > limit
	if more {
		models = models[:limit]
	}
	out := make([]domain.ProductOwnerDigest, 0, len(models))
	for _, m := range models {
		d := domain.ProductOwnerDigest{ID: m.ID, ProjectID: m.ProjectID, Date: m.Date, PeriodStart: m.PeriodStart, PeriodEnd: m.PeriodEnd, Summary: m.Summary, Items: []domain.ProductOwnerDigestItem{}}
		if err := json.Unmarshal([]byte(m.Items), &d.Items); err != nil {
			return nil, false, err
		}
		out = append(out, d)
	}
	return out, more, nil
}

// DigestState implements ports.ProductOwnerDigestStore.
func (r *ProductOwnerDigestRepository) DigestState(ctx context.Context, projectID string) (*domain.ProductOwnerDigestState, error) {
	var m ProductOwnerDigestStateModel
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &domain.ProductOwnerDigestState{ProjectID: m.ProjectID, SinceAt: m.SinceAt, CheckedDate: m.CheckedDate}, nil
}

// SaveDigestState implements ports.ProductOwnerDigestStore.
func (r *ProductOwnerDigestRepository) SaveDigestState(ctx context.Context, s domain.ProductOwnerDigestState) error {
	m := ProductOwnerDigestStateModel{ProjectID: s.ProjectID, SinceAt: s.SinceAt, CheckedDate: s.CheckedDate}
	return r.db.WithContext(ctx).Save(&m).Error
}
