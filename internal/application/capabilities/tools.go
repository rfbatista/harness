package capabilities

import "operators-mcp/internal/domain"

// User-defined tools, persisted. Code-defined tools are the tooling registry's.

func (s *Service) ListTools() []*domain.Tool {
	return s.tools.List()
}

func (s *Service) GetTool(id string) *domain.Tool {
	return s.tools.Get(id)
}

func (s *Service) CreateTool(name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	return s.tools.Create(name, description, inputSchema)
}

func (s *Service) UpdateTool(id, name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	return s.tools.Update(id, name, description, inputSchema)
}

func (s *Service) DeleteTool(id string) error {
	if s.tools.Get(id) == nil {
		return &domain.StructuredError{Code: "TOOL_NOT_FOUND", Message: "tool not found"}
	}
	return s.tools.Delete(id)
}
