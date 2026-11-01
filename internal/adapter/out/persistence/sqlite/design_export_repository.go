package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/ports"
)

var _ ports.DesignExportStore = (*DesignExportRepository)(nil)

// DesignExportModel is the GORM model of a repository's last design system
// export: one row per repository.
type DesignExportModel struct {
	RepositoryID   string `gorm:"primaryKey;column:repository_id"`
	DesignSystemID string `gorm:"column:design_system_id"`
	Version        int
	Commit         string
	At             time.Time
	SHA            string `gorm:"column:sha"`
}

// TableName overrides the table name.
func (DesignExportModel) TableName() string { return "design_system_exports" }

// DesignExportRepository persists the last export of each repository in SQLite
// via GORM.
type DesignExportRepository struct {
	db *gorm.DB
}

// NewDesignExportRepository returns a new design export store.
func NewDesignExportRepository(db *gorm.DB) *DesignExportRepository {
	return &DesignExportRepository{db: db}
}

// Get implements ports.DesignExportStore.
func (r *DesignExportRepository) Get(ctx context.Context, repositoryID string) (*ports.DesignExportRecord, error) {
	var rows []DesignExportModel
	if err := r.db.WithContext(ctx).Where("repository_id = ?", repositoryID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	m := rows[0]
	return &ports.DesignExportRecord{RepositoryID: m.RepositoryID, DesignSystemID: m.DesignSystemID, Version: m.Version, Commit: m.Commit, At: m.At, SHA: m.SHA}, nil
}

// Set implements ports.DesignExportStore.
func (r *DesignExportRepository) Set(ctx context.Context, rec *ports.DesignExportRecord) error {
	m := DesignExportModel{RepositoryID: rec.RepositoryID, DesignSystemID: rec.DesignSystemID, Version: rec.Version, Commit: rec.Commit, At: rec.At, SHA: rec.SHA}
	return r.db.WithContext(ctx).Save(&m).Error
}

// Delete implements ports.DesignExportStore.
func (r *DesignExportRepository) Delete(ctx context.Context, repositoryID string) error {
	return r.db.WithContext(ctx).Where("repository_id = ?", repositoryID).Delete(&DesignExportModel{}).Error
}
