package sqlite

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
)

// Ensure WorkspaceRepository implements ports.WorkspaceRepository at compile time.
var _ ports.WorkspaceRepository = (*WorkspaceRepository)(nil)

// WorkspaceRepository persists workspaces in SQLite via GORM.
type WorkspaceRepository struct {
	db *gorm.DB
}

// NewWorkspaceRepository returns a new workspace repository.
func NewWorkspaceRepository(db *gorm.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

// Get returns the workspace by id, or nil if not found.
func (r *WorkspaceRepository) Get(id string) *domain.Workspace {
	var m WorkspaceModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

// ListByRepository returns all workspaces belonging to the given repository.
func (r *WorkspaceRepository) ListByRepository(repositoryID string) []*domain.Workspace {
	var models []WorkspaceModel
	if err := r.db.Where("repository_id = ?", repositoryID).Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Workspace, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// Create creates a workspace with a generated id.
func (r *WorkspaceRepository) Create(w domain.Workspace) (*domain.Workspace, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &WorkspaceModel{
		ID:           id,
		RepositoryID: w.RepositoryID,
		Name:         w.Name,
		Branch:       w.Branch,
		Path:         w.Path,
		BaseRef:      w.BaseRef,
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Delete removes a workspace by id.
func (r *WorkspaceRepository) Delete(id string) error {
	var m WorkspaceModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
		}
		return err
	}
	return r.db.Delete(&m).Error
}
