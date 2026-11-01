package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.DesignSystemStore = (*DesignSystemRepository)(nil)

// DesignSystemVersionModel is the GORM model of one design-system version:
// the whole record as JSON, and its summary. Rows are only ever created.
type DesignSystemVersionModel struct {
	ProjectID   string `gorm:"primaryKey;column:project_id"`
	Version     int    `gorm:"primaryKey;autoIncrement:false"`
	At          time.Time
	By          string
	Source      string
	ProposalID  string `gorm:"column:proposal_id"`
	Message     string
	SummaryJSON string `gorm:"column:summary_json"`
	RecordJSON  string `gorm:"column:record_json"`
}

// TableName overrides the table name.
func (DesignSystemVersionModel) TableName() string { return "design_system_versions" }

func (m *DesignSystemVersionModel) toDomain() (*domain.DesignSystemVersion, error) {
	v := &domain.DesignSystemVersion{
		ProjectID: m.ProjectID, Version: m.Version, At: m.At, By: m.By, Source: m.Source,
		ProposalID: m.ProposalID, Message: m.Message,
	}
	if err := json.Unmarshal([]byte(m.SummaryJSON), &v.Summary); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(m.RecordJSON), &v.Record); err != nil {
		return nil, err
	}
	v.Record.Normalize()
	return v, nil
}

// DesignSystemRepository persists design-system versions in SQLite via GORM.
type DesignSystemRepository struct {
	db *gorm.DB
}

// NewDesignSystemRepository returns a new design-system version store.
func NewDesignSystemRepository(db *gorm.DB) *DesignSystemRepository {
	return &DesignSystemRepository{db: db}
}

// latestVersion reads the project's newest row; nil when it has none. Find,
// not First: a miss is not an error worth logging.
func latestVersion(db *gorm.DB, projectID string) (*DesignSystemVersionModel, error) {
	var rows []DesignSystemVersionModel
	if err := db.Where("project_id = ?", projectID).Order("version DESC").Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// Latest implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Latest(ctx context.Context, projectID string) (*domain.DesignSystemVersion, error) {
	m, err := latestVersion(r.db.WithContext(ctx), projectID)
	if err != nil || m == nil {
		return nil, err
	}
	return m.toDomain()
}

// Get implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Get(ctx context.Context, projectID string, version int) (*domain.DesignSystemVersion, error) {
	var rows []DesignSystemVersionModel
	if err := r.db.WithContext(ctx).Where("project_id = ? AND version = ?", projectID, version).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0].toDomain()
}

// List implements ports.DesignSystemStore.
func (r *DesignSystemRepository) List(ctx context.Context, projectID string, limit, before int) ([]*domain.DesignSystemVersion, error) {
	q := r.db.WithContext(ctx).Where("project_id = ?", projectID)
	if before > 0 {
		q = q.Where("version < ?", before)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []DesignSystemVersionModel
	if err := q.Order("version DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.DesignSystemVersion, 0, len(rows))
	for i := range rows {
		v, err := rows[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Append implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Append(ctx context.Context, v *domain.DesignSystemVersion, baseVersion int) error {
	record, err := json.Marshal(v.Record)
	if err != nil {
		return err
	}
	summary, err := json.Marshal(v.Summary)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cur, err := latestVersion(tx, v.ProjectID)
		if err != nil {
			return err
		}
		stored := 0
		if cur != nil {
			stored = cur.Version
		}
		if stored != baseVersion {
			return &domain.DetailedError{
				StructuredError: &domain.StructuredError{Code: "DESIGN_SYSTEM_CHANGED", Message: "the design system was saved since you loaded it"},
				Details:         map[string]any{"current_version": stored},
			}
		}
		m := DesignSystemVersionModel{
			ProjectID: v.ProjectID, Version: stored + 1, At: v.At, By: v.By, Source: v.Source, ProposalID: v.ProposalID,
			Message: v.Message, SummaryJSON: string(summary), RecordJSON: string(record),
		}
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		v.Version = m.Version
		return nil
	})
}

// DeleteByProject implements ports.DesignSystemStore.
func (r *DesignSystemRepository) DeleteByProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Where("project_id = ?", projectID).Delete(&DesignSystemVersionModel{}).Error
}
