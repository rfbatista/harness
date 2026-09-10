package sqlite

import (
	"time"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"

	"gorm.io/gorm"
)

var _ ports.MCPServerRepository = (*MCPServerRepository)(nil)

// MCPServerRepository persists MCP server configurations in SQLite via GORM.
type MCPServerRepository struct {
	db *gorm.DB
}

func NewMCPServerRepository(db *gorm.DB) *MCPServerRepository {
	return &MCPServerRepository{db: db}
}

func (r *MCPServerRepository) Get(id string) *domain.MCPServer {
	var m MCPServerModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *MCPServerRepository) List() []*domain.MCPServer {
	var models []MCPServerModel
	if err := r.db.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.MCPServer, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *MCPServerRepository) Create(in domain.MCPServerInput) (*domain.MCPServer, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &MCPServerModel{
		ID:          id,
		Name:        in.Name,
		Description: in.Description,
		Transport:   in.Transport,
		Command:     in.Command,
		Args:        stringSlice(in.Args),
		URL:         in.URL,
		Env:         stringMap(in.Env),
		Headers:     stringMap(in.Headers),
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *MCPServerRepository) Update(id string, in domain.MCPServerInput) (*domain.MCPServer, error) {
	var m MCPServerModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "mcp server not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{
		"name":        in.Name,
		"description": in.Description,
		"transport":   in.Transport,
		"command":     in.Command,
		"args":        stringSlice(in.Args),
		"url":         in.URL,
		"env":         stringMap(in.Env),
		"headers":     stringMap(in.Headers),
	}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *MCPServerRepository) UpdateProbeResult(id string, result domain.MCPProbeResult) (*domain.MCPServer, error) {
	var m MCPServerModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "mcp server not found"}
		}
		return nil, err
	}
	now := time.Now()
	updates := map[string]interface{}{
		"last_probe_at":     &now,
		"last_probe_status": result.Status,
		"last_probe_error":  result.Error,
		"tool_count":        result.ToolCount,
		"resource_count":    result.ResourceCount,
		"prompt_count":      result.PromptCount,
	}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *MCPServerRepository) Delete(id string) error {
	var m MCPServerModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "mcp server not found"}
		}
		return err
	}
	return r.db.Delete(&m).Error
}
