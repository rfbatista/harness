package sqlite

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
)

var _ ports.ToolRepository = (*ToolRepository)(nil)

type ToolRepository struct {
	db *gorm.DB
}

func NewToolRepository(db *gorm.DB) *ToolRepository {
	return &ToolRepository{db: db}
}

func (r *ToolRepository) Get(id string) *domain.Tool {
	var m ToolModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *ToolRepository) List() []*domain.Tool {
	var models []ToolModel
	if err := r.db.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Tool, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *ToolRepository) Create(name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &ToolModel{
		ID:          id,
		Name:        name,
		Description: description,
		InputSchema: jsonMap(inputSchema),
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *ToolRepository) Update(id, name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	var m ToolModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "TOOL_NOT_FOUND", Message: "tool not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{
		"name":         name,
		"description":  description,
		"input_schema": jsonMap(inputSchema),
	}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *ToolRepository) Delete(id string) error {
	var m ToolModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "TOOL_NOT_FOUND", Message: "tool not found"}
		}
		return err
	}
	return r.db.Delete(&m).Error
}
