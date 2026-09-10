package sqlite

import (
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"

	"gorm.io/gorm"
)

// Ensure ProjectRepository implements ports.ProjectRepository at compile time.
var _ ports.ProjectRepository = (*ProjectRepository)(nil)

// ProjectRepository persists projects in SQLite via GORM.
type ProjectRepository struct {
	db *gorm.DB
}

// NewProjectRepository returns a new project repository.
func NewProjectRepository(db *gorm.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

// Get returns the project by id, or nil if not found.
func (r *ProjectRepository) Get(id string) *domain.Project {
	var m ProjectModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return nil
	}
	return m.ToDomain()
}

// List returns all projects.
func (r *ProjectRepository) List() []*domain.Project {
	var models []ProjectModel
	if err := r.db.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Project, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// Create creates a project with generated id. RootDir is required.
func (r *ProjectRepository) Create(name, rootDir string) (*domain.Project, error) {
	if rootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "project root directory is required"}
	}
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &ProjectModel{
		ID:           id,
		Name:         name,
		RootDir:      rootDir,
		IgnoredPaths: stringSlice{},
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Update updates a project by id.
func (r *ProjectRepository) Update(id, name, rootDir string) (*domain.Project, error) {
	var m ProjectModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{}
	if name != "" {
		updates["name"] = name
	}
	if rootDir != "" {
		updates["root_dir"] = rootDir
	}
	if len(updates) > 0 {
		if err := r.db.Model(&m).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	// Reload to get updated row
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Delete removes a project by id. Caller should delete zones for the project first (e.g. via ZoneRepository.DeleteByProject).
func (r *ProjectRepository) Delete(projectID string) error {
	var m ProjectModel
	if err := r.db.First(&m, "id = ?", projectID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
		}
		return err
	}
	if err := r.db.Delete(&m).Error; err != nil {
		return err
	}
	return nil
}

// AddIgnoredPath adds path to the project's ignored list (no-op if already present).
func (r *ProjectRepository) AddIgnoredPath(projectID, path string) (*domain.Project, error) {
	path = domain.NormalizePath(path)
	if path == "" {
		return nil, &domain.StructuredError{Code: "INVALID_PATH", Message: "path is required"}
	}
	var m ProjectModel
	if err := r.db.First(&m, "id = ?", projectID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
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

// RemoveIgnoredPath removes path from the project's ignored list.
func (r *ProjectRepository) RemoveIgnoredPath(projectID, path string) (*domain.Project, error) {
	path = domain.NormalizePath(path)
	var m ProjectModel
	if err := r.db.First(&m, "id = ?", projectID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
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
