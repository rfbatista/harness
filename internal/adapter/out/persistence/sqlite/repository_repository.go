package sqlite

import (
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"

	"gorm.io/gorm"
)

// Ensure RepositoryRepository implements ports.RepositoryRepository at compile time.
var _ ports.RepositoryRepository = (*RepositoryRepository)(nil)

// RepositoryRepository persists repositories in SQLite via GORM.
type RepositoryRepository struct {
	db *gorm.DB
}

// NewRepositoryRepository returns a new repository repository.
func NewRepositoryRepository(db *gorm.DB) *RepositoryRepository {
	return &RepositoryRepository{db: db}
}

// Get returns the repository by id, or nil if not found.
func (r *RepositoryRepository) Get(id string) *domain.Repository {
	var m RepositoryModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

// ListByProject returns all repositories belonging to the given project.
func (r *RepositoryRepository) ListByProject(projectID string) []*domain.Repository {
	var models []RepositoryModel
	if err := r.db.Where("project_id = ?", projectID).Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Repository, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// Create creates a repository with generated id. URL is required.
func (r *RepositoryRepository) Create(projectID, name, description, url, rootDir string) (*domain.Repository, error) {
	if url == "" {
		return nil, &domain.StructuredError{Code: "INVALID_URL", Message: "repository url is required"}
	}
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &RepositoryModel{
		ID:           id,
		ProjectID:    projectID,
		Name:         name,
		Description:  description,
		URL:          url,
		RootDir:      rootDir,
		IgnoredPaths: stringSlice{},
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Update updates a repository by id.
func (r *RepositoryRepository) Update(id, name, description, url, rootDir string) (*domain.Repository, error) {
	var m RepositoryModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{}
	if name != "" {
		updates["name"] = name
	}
	if description != "" {
		updates["description"] = description
	}
	if url != "" {
		updates["url"] = url
	}
	if rootDir != "" {
		updates["root_dir"] = rootDir
	}
	if len(updates) > 0 {
		if err := r.db.Model(&m).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// DeleteByProject deletes all repositories for the given project.
func (r *RepositoryRepository) DeleteByProject(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&RepositoryModel{}).Error
}

// Delete removes a repository by id.
func (r *RepositoryRepository) Delete(id string) error {
	var m RepositoryModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
		}
		return err
	}
	return r.db.Delete(&m).Error
}

// AddIgnoredPath adds path to the repository's ignored list (no-op if already present).
func (r *RepositoryRepository) AddIgnoredPath(repositoryID, path string) (*domain.Repository, error) {
	path = domain.NormalizePath(path)
	if path == "" {
		return nil, &domain.StructuredError{Code: "INVALID_PATH", Message: "path is required"}
	}
	var m RepositoryModel
	if err := r.db.First(&m, "id = ?", repositoryID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
		}
		return nil, err
	}
	for _, ig := range m.IgnoredPaths {
		if ig == path {
			return m.ToDomain(), nil
		}
	}
	m.IgnoredPaths = append(m.IgnoredPaths, path)
	if err := r.db.Model(&m).Update("ignored_paths", m.IgnoredPaths).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// RemoveIgnoredPath removes path from the repository's ignored list.
func (r *RepositoryRepository) RemoveIgnoredPath(repositoryID, path string) (*domain.Repository, error) {
	path = domain.NormalizePath(path)
	var m RepositoryModel
	if err := r.db.First(&m, "id = ?", repositoryID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
		}
		return nil, err
	}
	var filtered []string
	for _, ig := range m.IgnoredPaths {
		if ig != path {
			filtered = append(filtered, ig)
		}
	}
	m.IgnoredPaths = stringSlice(filtered)
	if err := r.db.Model(&m).Update("ignored_paths", m.IgnoredPaths).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}
