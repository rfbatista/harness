package sqlite

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
)

var _ ports.PromptRepository = (*PromptRepository)(nil)

// PromptRepository persists prompts in SQLite via GORM.
type PromptRepository struct {
	db *gorm.DB
}

// NewPromptRepository returns a new prompt repository.
func NewPromptRepository(db *gorm.DB) *PromptRepository {
	return &PromptRepository{db: db}
}

func (r *PromptRepository) Get(id string) *domain.Prompt {
	var m PromptModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *PromptRepository) List() []*domain.Prompt {
	var models []PromptModel
	if err := r.db.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Prompt, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *PromptRepository) Create(name, description, content string) (*domain.Prompt, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &PromptModel{ID: id, Name: name, Description: description, Content: content}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *PromptRepository) Update(id, name, description, content string) (*domain.Prompt, error) {
	var m PromptModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "PROMPT_NOT_FOUND", Message: "prompt not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{"name": name, "description": description, "content": content}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *PromptRepository) Delete(id string) error {
	var m PromptModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "PROMPT_NOT_FOUND", Message: "prompt not found"}
		}
		return err
	}
	return r.db.Delete(&m).Error
}
