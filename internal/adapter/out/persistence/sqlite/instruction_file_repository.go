package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// InstructionFileModel is the GORM model of one instruction file of a
// repository, kept apart from the architecture record.
type InstructionFileModel struct {
	RepositoryID string `gorm:"primaryKey;column:repository_id"`
	Path         string `gorm:"primaryKey;column:path"`
	ProjectID    string `gorm:"column:project_id;index"`
	Kind         string
	Version      int
	Content      string
	UpdatedAt    time.Time
}

// TableName overrides the table name.
func (InstructionFileModel) TableName() string { return "instruction_files" }

func (m InstructionFileModel) toDomain() domain.InstructionFile {
	return domain.InstructionFile{
		RepositoryID: m.RepositoryID, ProjectID: m.ProjectID, Path: m.Path, Kind: m.Kind,
		Version: m.Version, Size: len(m.Content), Content: m.Content, UpdatedAt: m.UpdatedAt,
	}
}

func findInstructionFile(db *gorm.DB, repositoryID, path string) (*InstructionFileModel, error) {
	var rows []InstructionFileModel
	if err := db.Where("repository_id = ? AND path = ?", repositoryID, path).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func instructionNotFound(path string) error {
	return &domain.StructuredError{Code: "INSTRUCTION_NOT_FOUND", Message: "no instruction file " + path}
}

func instructionChanged(path string) error {
	return &domain.StructuredError{Code: "INSTRUCTION_CHANGED", Message: path + " was saved since you loaded it"}
}

func instructionExists(path string) error {
	return &domain.StructuredError{Code: "INSTRUCTION_EXISTS", Message: "an instruction file " + path + " already exists"}
}

// ListInstructionFiles implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) ListInstructionFiles(ctx context.Context, repositoryID string) ([]domain.InstructionFile, error) {
	var rows []InstructionFileModel
	if err := r.db.WithContext(ctx).Where("repository_id = ?", repositoryID).Order("path").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.InstructionFile, 0, len(rows))
	for _, m := range rows {
		out = append(out, m.toDomain())
	}
	return out, nil
}

// GetInstructionFile implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) GetInstructionFile(ctx context.Context, repositoryID, path string) (*domain.InstructionFile, error) {
	m, err := findInstructionFile(r.db.WithContext(ctx), repositoryID, path)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, instructionNotFound(path)
	}
	f := m.toDomain()
	return &f, nil
}

// totalSize is what the repository's files hold, without the one at except.
func totalSize(db *gorm.DB, repositoryID, except string) (int, error) {
	var n int
	err := db.Model(&InstructionFileModel{}).Where("repository_id = ? AND path <> ?", repositoryID, except).
		Select("COALESCE(SUM(LENGTH(CAST(content AS BLOB))), 0)").Scan(&n).Error
	return n, err
}

// SaveInstructionFile implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) SaveInstructionFile(ctx context.Context, f domain.InstructionFile, baseVersion, totalLimit int) (*domain.InstructionFile, error) {
	var out *domain.InstructionFile
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := findInstructionFile(tx, f.RepositoryID, f.Path)
		if err != nil {
			return err
		}
		switch {
		case m == nil && baseVersion != 0:
			return instructionNotFound(f.Path)
		case m != nil && baseVersion == 0:
			return instructionExists(f.Path)
		case m != nil && m.Version != baseVersion:
			return instructionChanged(f.Path)
		}
		if m == nil {
			m = &InstructionFileModel{RepositoryID: f.RepositoryID, Path: f.Path}
		}
		others, err := totalSize(tx, f.RepositoryID, f.Path)
		if err != nil {
			return err
		}
		if others+len(f.Content) > totalLimit {
			return &domain.DetailedError{
				StructuredError: &domain.StructuredError{Code: "INSTRUCTION_INVALID", Message: "the repository's instruction files would be over the limit"},
				Details:         map[string]any{"errors": []domain.ArchFieldError{{Path: "/content", Code: domain.ArchInvalidValue, Message: "all instruction files of a repository together are at most 3.75 MiB (4 MiB less 256 KB kept for ARCHITECTURE.md)"}}},
			}
		}
		m.ProjectID, m.Kind, m.Version, m.Content, m.UpdatedAt = f.ProjectID, f.Kind, m.Version+1, f.Content, time.Now()
		if err := tx.Save(m).Error; err != nil {
			return err
		}
		d := m.toDomain()
		out = &d
		return nil
	})
	return out, err
}

// RenameInstructionFile implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) RenameInstructionFile(ctx context.Context, repositoryID, path, newPath string, baseVersion int) (*domain.InstructionFile, error) {
	var out *domain.InstructionFile
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := findInstructionFile(tx, repositoryID, path)
		if err != nil {
			return err
		}
		if m == nil {
			return instructionNotFound(path)
		}
		if m.Version != baseVersion {
			return instructionChanged(path)
		}
		if other, err := findInstructionFile(tx, repositoryID, newPath); err != nil {
			return err
		} else if other != nil {
			return instructionExists(newPath)
		}
		moved := *m
		moved.Path, moved.UpdatedAt = newPath, time.Now()
		if err := tx.Delete(m).Error; err != nil {
			return err
		}
		if err := tx.Create(&moved).Error; err != nil {
			return err
		}
		d := moved.toDomain()
		out = &d
		return nil
	})
	return out, err
}

// DeleteInstructionFile implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) DeleteInstructionFile(ctx context.Context, repositoryID, path string, baseVersion int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := findInstructionFile(tx, repositoryID, path)
		if err != nil {
			return err
		}
		if m == nil {
			return instructionNotFound(path)
		}
		if m.Version != baseVersion {
			return instructionChanged(path)
		}
		return tx.Delete(m).Error
	})
}

// SetRecord implements ports.ArchitectureRecordStore.
func (r *ArchitectureRecordRepository) SetRecord(ctx context.Context, repositoryID string, record domain.ArchitectureRecord) (*ports.StoredArchitecture, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var out *ports.StoredArchitecture
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := findRecord(tx, repositoryID)
		if err != nil || m == nil {
			return err
		}
		m.Version, m.RecordJSON, m.UpdatedAt = m.Version+1, string(raw), time.Now()
		if err := tx.Save(m).Error; err != nil {
			return err
		}
		out, err = m.toStored()
		return err
	})
	return out, err
}
