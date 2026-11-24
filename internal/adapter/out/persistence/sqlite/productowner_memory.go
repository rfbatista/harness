package sqlite

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
)

// ProductOwnerMemoryEntryModel is the GORM model of a project's memory entry
// (ADR 0111).
type ProductOwnerMemoryEntryModel struct {
	ID        string `gorm:"primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	Text      string
	SavedAt   time.Time
	ChangedAt *time.Time
	ChangedBy string `gorm:"column:changed_by"`
	ThreadID  string `gorm:"column:thread_id"`
}

// TableName overrides the table name.
func (ProductOwnerMemoryEntryModel) TableName() string { return "product_owner_memory_entries" }

// ProductOwnerMemoryLogModel is one line of a project's memory log. Its
// autoincrement Version orders the changes of every project; a project's
// version is the last one of its lines.
type ProductOwnerMemoryLogModel struct {
	Version   int64  `gorm:"primaryKey;autoIncrement"`
	ProjectID string `gorm:"column:project_id;index"`
	EntryID   string `gorm:"column:entry_id"`
	Op        string
	Text      string
	By        string `gorm:"column:by_who"`
	ThreadID  string `gorm:"column:thread_id"`
	At        time.Time
}

// TableName overrides the table name.
func (ProductOwnerMemoryLogModel) TableName() string { return "product_owner_memory_log" }

func (m ProductOwnerMemoryEntryModel) domain() domain.MemoryEntry {
	return domain.MemoryEntry{ID: m.ID, ProjectID: m.ProjectID, Text: m.Text, SavedAt: m.SavedAt, ChangedAt: m.ChangedAt, ChangedBy: m.ChangedBy, ThreadID: m.ThreadID}
}

func memoryEntryModel(e domain.MemoryEntry) ProductOwnerMemoryEntryModel {
	return ProductOwnerMemoryEntryModel{ID: e.ID, ProjectID: e.ProjectID, Text: e.Text, SavedAt: e.SavedAt, ChangedAt: e.ChangedAt, ChangedBy: e.ChangedBy, ThreadID: e.ThreadID}
}

// MemoryEntries implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) MemoryEntries(ctx context.Context, projectID string) ([]domain.MemoryEntry, error) {
	var ms []ProductOwnerMemoryEntryModel
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("saved_at DESC, id DESC").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.MemoryEntry, len(ms))
	for i, m := range ms {
		out[i] = m.domain()
	}
	return out, nil
}

// MemoryEntry implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) MemoryEntry(ctx context.Context, id string) (*domain.MemoryEntry, error) {
	var m ProductOwnerMemoryEntryModel
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &domain.StructuredError{Code: "MEMORY_ENTRY_NOT_FOUND", Message: "MEMORY_ENTRY_NOT_FOUND: no memory entry " + id}
	}
	if err != nil {
		return nil, err
	}
	e := m.domain()
	return &e, nil
}

// ApplyMemoryChange implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) ApplyMemoryChange(ctx context.Context, entry domain.MemoryEntry, change domain.MemoryChange) (domain.MemoryChange, error) {
	line := ProductOwnerMemoryLogModel{ProjectID: change.ProjectID, EntryID: change.EntryID, Op: change.Op, Text: change.Text,
		By: change.By, ThreadID: change.ThreadID, At: change.At}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		switch change.Op {
		case domain.MemoryOpDropped:
			if err := tx.Where("id = ?", entry.ID).Delete(&ProductOwnerMemoryEntryModel{}).Error; err != nil {
				return err
			}
		default:
			m := memoryEntryModel(entry)
			if err := tx.Save(&m).Error; err != nil {
				return err
			}
		}
		return tx.Create(&line).Error
	})
	if err != nil {
		return change, err
	}
	change.Version = line.Version
	return change, nil
}

// MemoryVersion implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) MemoryVersion(ctx context.Context, projectID string) (int64, error) {
	var v int64
	err := r.db.WithContext(ctx).Model(&ProductOwnerMemoryLogModel{}).Where("project_id = ?", projectID).Select("COALESCE(MAX(version), 0)").Scan(&v).Error
	return v, err
}

// MemoryChangesSince implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) MemoryChangesSince(ctx context.Context, projectID string, version int64) ([]domain.MemoryChange, error) {
	var ms []ProductOwnerMemoryLogModel
	if err := r.db.WithContext(ctx).Where("project_id = ? AND version > ?", projectID, version).Order("version").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.MemoryChange, len(ms))
	for i, m := range ms {
		out[i] = domain.MemoryChange{Version: m.Version, ProjectID: m.ProjectID, EntryID: m.EntryID, Op: m.Op, Text: m.Text, By: m.By, ThreadID: m.ThreadID, At: m.At}
	}
	return out, nil
}
