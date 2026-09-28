package sqlite

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
)

// Ensure AgentRepository implements ports.AgentRepository at compile time.
var _ ports.AgentRepository = (*AgentRepository)(nil)

// AgentRepository persists agents in SQLite via GORM.
type AgentRepository struct {
	db *gorm.DB
}

// NewAgentRepository returns a new agent repository.
func NewAgentRepository(db *gorm.DB) *AgentRepository {
	return &AgentRepository{db: db}
}

// Get returns the agent by id, or nil if not found.
func (r *AgentRepository) Get(id string) *domain.Agent {
	var m AgentModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return nil
	}
	return m.ToDomain()
}

// List returns all agents.
func (r *AgentRepository) List() []*domain.Agent {
	var models []AgentModel
	if err := r.db.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Agent, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// Create creates an agent with generated id.
func (r *AgentRepository) Create(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &AgentModel{
		ID:           id,
		Name:         name,
		Description:  description,
		PromptID:     promptID,
		SkillIDs:     stringSlice(skillIDs),
		MCPServerIDs: stringSlice(mcpServerIDs),
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Update updates an agent by id.
func (r *AgentRepository) Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	var m AgentModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{
		"name":           name,
		"description":    description,
		"prompt_id":      promptID,
		"skill_ids":      stringSlice(skillIDs),
		"mcp_server_ids": stringSlice(mcpServerIDs),
	}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Delete removes an agent by id.
func (r *AgentRepository) Delete(id string) error {
	var m AgentModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
		}
		return err
	}
	return r.db.Delete(&m).Error
}
