package sqlite

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
)

// Ensure BoundedContextRepository implements ports.BoundedContextRepository at compile time.
var _ ports.BoundedContextRepository = (*BoundedContextRepository)(nil)

// BoundedContextRepository persists bounded contexts in SQLite via GORM.
type BoundedContextRepository struct {
	db *gorm.DB
}

// NewBoundedContextRepository returns a new bounded context repository.
func NewBoundedContextRepository(db *gorm.DB) *BoundedContextRepository {
	return &BoundedContextRepository{db: db}
}

// Get returns the bounded context by id, or nil if not found.
func (r *BoundedContextRepository) Get(id string) *domain.BoundedContext {
	var m BoundedContextModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

// ListByProject returns all bounded contexts for the given project.
func (r *BoundedContextRepository) ListByProject(projectID string) []*domain.BoundedContext {
	var models []BoundedContextModel
	if err := r.db.Where("project_id = ?", projectID).Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.BoundedContext, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// Create creates a bounded context in the given project and returns it with generated id.
func (r *BoundedContextRepository) Create(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	if name == "" {
		return nil, &domain.StructuredError{Code: "INVALID_NAME", Message: "bounded context name is required"}
	}
	id, err := genID()
	if err != nil {
		return nil, err
	}
	termsCopy := cloneTerms(terms)
	if termsCopy == nil {
		termsCopy = []domain.LanguageTerm{}
	}
	m := &BoundedContextModel{
		ID:                 id,
		ProjectID:          projectID,
		Name:               name,
		Purpose:            purpose,
		UbiquitousLanguage: languageTermSlice(termsCopy),
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Update updates a bounded context by id. The language terms are replaced wholesale.
func (r *BoundedContextRepository) Update(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	var m BoundedContextModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "BOUNDED_CONTEXT_NOT_FOUND", Message: "bounded context not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{
		"purpose":             purpose,
		"ubiquitous_language": languageTermSlice(cloneTerms(terms)),
	}
	if name != "" {
		updates["name"] = name
	}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Delete removes a bounded context by id.
func (r *BoundedContextRepository) Delete(id string) error {
	res := r.db.Delete(&BoundedContextModel{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "BOUNDED_CONTEXT_NOT_FOUND", Message: "bounded context not found"}
	}
	return nil
}

// DeleteByProject deletes all bounded contexts for the given project.
func (r *BoundedContextRepository) DeleteByProject(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&BoundedContextModel{}).Error
}

func cloneTerms(t []domain.LanguageTerm) []domain.LanguageTerm {
	if t == nil {
		return nil
	}
	return append([]domain.LanguageTerm(nil), t...)
}
