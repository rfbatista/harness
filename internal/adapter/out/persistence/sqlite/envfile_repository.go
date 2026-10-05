package sqlite

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.EnvFileRepository = (*EnvFileRepository)(nil)

// EnvFileRepository stores repositories' env files.
type EnvFileRepository struct{ db *gorm.DB }

// NewEnvFileRepository returns the store.
func NewEnvFileRepository(db *gorm.DB) *EnvFileRepository { return &EnvFileRepository{db: db} }

// List returns a repository's env files by path.
func (r *EnvFileRepository) List(repositoryID string) []*domain.EnvFile {
	var models []EnvFileModel
	r.db.Where("repository_id = ?", repositoryID).Order("path").Find(&models)
	out := make([]*domain.EnvFile, 0, len(models))
	for _, m := range models {
		out = append(out, &domain.EnvFile{RepositoryID: m.RepositoryID, Path: m.Path, Content: m.Content, UpdatedAt: m.UpdatedAt})
	}
	return out
}

// Put creates or replaces the file at its repository and path.
func (r *EnvFileRepository) Put(f *domain.EnvFile) (*domain.EnvFile, error) {
	m := EnvFileModel{RepositoryID: f.RepositoryID, Path: f.Path, Content: f.Content, UpdatedAt: time.Now()}
	err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "repository_id"}, {Name: "path"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at"}),
	}).Create(&m).Error
	if err != nil {
		return nil, err
	}
	return &domain.EnvFile{RepositoryID: m.RepositoryID, Path: m.Path, Content: m.Content, UpdatedAt: m.UpdatedAt}, nil
}

// Delete removes one file, or ENV_FILE_NOT_FOUND.
func (r *EnvFileRepository) Delete(repositoryID, path string) error {
	res := r.db.Where("repository_id = ? AND path = ?", repositoryID, path).Delete(&EnvFileModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "ENV_FILE_NOT_FOUND", Message: "env file " + path + " not found"}
	}
	return nil
}

// DeleteByRepository removes every env file of a repository.
func (r *EnvFileRepository) DeleteByRepository(repositoryID string) error {
	err := r.db.Where("repository_id = ?", repositoryID).Delete(&EnvFileModel{}).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}
