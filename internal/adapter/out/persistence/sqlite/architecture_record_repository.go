package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ArchitectureRecordStore = (*ArchitectureRecordRepository)(nil)

// ArchitectureRecordModel is the GORM model of a repository's architecture
// record: the whole record as JSON, and what the last export wrote.
type ArchitectureRecordModel struct {
	RepositoryID    string `gorm:"primaryKey;column:repository_id"`
	ProjectID       string `gorm:"column:project_id;index"`
	Version         int
	RecordJSON      string `gorm:"column:record_json"`
	UpdatedAt       time.Time
	ExportedVersion int        `gorm:"column:exported_version"`
	ExportedCommit  string     `gorm:"column:exported_commit"`
	ExportedAt      *time.Time `gorm:"column:exported_at"`
	ExportedSHA     string     `gorm:"column:exported_sha"`
}

// TableName overrides the table name.
func (ArchitectureRecordModel) TableName() string { return "architecture_records" }

func (m *ArchitectureRecordModel) toStored() (*ports.StoredArchitecture, error) {
	var rec domain.ArchitectureRecord
	if err := json.Unmarshal([]byte(m.RecordJSON), &rec); err != nil {
		return nil, err
	}
	return &ports.StoredArchitecture{
		RepositoryID: m.RepositoryID, ProjectID: m.ProjectID, Version: m.Version, Record: rec, UpdatedAt: m.UpdatedAt,
		ExportedVersion: m.ExportedVersion, ExportedCommit: m.ExportedCommit, ExportedAt: m.ExportedAt, ExportedSHA: m.ExportedSHA,
	}, nil
}

// ArchitectureRecordRepository persists records in SQLite via GORM.
type ArchitectureRecordRepository struct {
	db *gorm.DB
}

// NewArchitectureRecordRepository returns a new record repository.
func NewArchitectureRecordRepository(db *gorm.DB) *ArchitectureRecordRepository {
	return &ArchitectureRecordRepository{db: db}
}

// findRecord reads one row; nil when there is none. Find, not First: a miss
// is not an error worth logging.
func findRecord(db *gorm.DB, repositoryID string) (*ArchitectureRecordModel, error) {
	var rows []ArchitectureRecordModel
	if err := db.Where("repository_id = ?", repositoryID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// Get implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) Get(ctx context.Context, repositoryID string) (*ports.StoredArchitecture, error) {
	m, err := findRecord(r.db.WithContext(ctx), repositoryID)
	if err != nil || m == nil {
		return nil, err
	}
	return m.toStored()
}

// Save implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) Save(ctx context.Context, repositoryID, projectID string, baseVersion int, record domain.ArchitectureRecord, zones []*domain.Zone) (*ports.StoredArchitecture, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var out *ports.StoredArchitecture
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := findRecord(tx, repositoryID)
		if err != nil {
			return err
		}
		stored := 0
		if m == nil {
			m = &ArchitectureRecordModel{}
		} else {
			stored = m.Version
		}
		if stored != baseVersion {
			return &domain.StructuredError{Code: "ARCHITECTURE_CHANGED", Message: "the architecture was saved since you loaded it"}
		}
		m.RepositoryID, m.ProjectID, m.Version, m.RecordJSON, m.UpdatedAt = repositoryID, projectID, stored+1, string(raw), time.Now()
		if err := tx.Save(m).Error; err != nil {
			return err
		}
		if err := tx.Where("repository_id = ?", repositoryID).Delete(&ZoneModel{}).Error; err != nil {
			return err
		}
		for _, z := range zones {
			id, err := genID()
			if err != nil {
				return err
			}
			z.ID = id
			zm := zoneModelOf(z)
			if err := tx.Create(&zm).Error; err != nil {
				return err
			}
		}
		stored2, err := m.toStored()
		out = stored2
		return err
	})
	return out, err
}

// Delete implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) Delete(ctx context.Context, repositoryID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("repository_id = ?", repositoryID).Delete(&ZoneModel{}).Error; err != nil {
			return err
		}
		return tx.Where("repository_id = ?", repositoryID).Delete(&ArchitectureRecordModel{}).Error
	})
}

// DeleteByProject implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) DeleteByProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Where("project_id = ?", projectID).Delete(&ArchitectureRecordModel{}).Error
	})
}
