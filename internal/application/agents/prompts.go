package agents

import "operators-mcp/internal/domain"

// ListPrompts returns all prompts.
func (s *Service) ListPrompts() []*domain.Prompt {
	return s.prompts.List()
}

// GetPrompt returns one prompt by id, or nil if not found.
func (s *Service) GetPrompt(id string) *domain.Prompt {
	return s.prompts.Get(id)
}

// CreatePrompt creates a prompt with the given name, description, and content.
func (s *Service) CreatePrompt(name, description, content string) (*domain.Prompt, error) {
	return s.prompts.Create(name, description, content)
}

// UpdatePrompt updates an existing prompt.
func (s *Service) UpdatePrompt(id, name, description, content string) (*domain.Prompt, error) {
	return s.prompts.Update(id, name, description, content)
}

// DeletePrompt deletes a prompt after unlinking it from every agent built on
// it, and announces PromptDeleted so zones drop it from their rules.
func (s *Service) DeletePrompt(id string) error {
	if s.prompts.Get(id) == nil {
		return &domain.StructuredError{Code: "PROMPT_NOT_FOUND", Message: "prompt not found"}
	}
	for _, a := range s.agents.List() {
		if a.PromptID == id {
			if _, err := s.agents.Update(a.ID, a.Name, a.Description, "", a.SkillIDs, a.MCPServerIDs); err != nil {
				return err
			}
		}
	}
	if err := s.prompts.Delete(id); err != nil {
		return err
	}
	return s.publish(domain.PromptDeleted{PromptID: id})
}
