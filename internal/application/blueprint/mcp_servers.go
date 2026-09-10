package blueprint

import (
	"context"

	"operators-mcp/internal/domain"
)

// The MCP server use cases live in harnesskit/mcpserver; this file delegates to
// them and supplies the one thing the library cannot know: which agents
// reference a server.

// ListMCPServers returns all MCP server configurations with usage counts.
func (s *Service) ListMCPServers() []*domain.MCPServer { return s.mcpSvc.List() }

// GetMCPServer returns one MCP server by id, or nil if not found.
func (s *Service) GetMCPServer(id string) *domain.MCPServer { return s.mcpSvc.Get(id) }

// CreateMCPServer creates an MCP server configuration.
func (s *Service) CreateMCPServer(in domain.MCPServerInput) (*domain.MCPServer, error) {
	return s.mcpSvc.Create(in)
}

// UpdateMCPServer applies a partial change to an MCP server configuration.
func (s *Service) UpdateMCPServer(id string, in domain.MCPServerInput) (*domain.MCPServer, error) {
	return s.mcpSvc.Update(id, in)
}

// UpdateMCPServerProbeResult persists probe metadata for a server.
func (s *Service) UpdateMCPServerProbeResult(id string, result domain.MCPProbeResult) (*domain.MCPServer, error) {
	return s.mcpSvc.UpdateProbeResult(id, result)
}

// ImportMCPServers imports servers from Cursor-style JSON.
func (s *Service) ImportMCPServers(content, onDuplicate string) ([]*domain.MCPServer, error) {
	return s.mcpSvc.Import(content, onDuplicate)
}

// ProbeMCPServer connects to a server and reports what it exposes.
func (s *Service) ProbeMCPServer(ctx context.Context, server domain.MCPServer) (domain.MCPProbeResult, error) {
	return s.mcpSvc.Probe(ctx, server)
}

// DeleteMCPServer deletes an MCP server configuration by id.
func (s *Service) DeleteMCPServer(id string) error { return s.mcpSvc.Delete(id) }

// enrichMCPServerUsage fills the denormalized usage read model. It backs
// mcpserver.Hooks.Decorate: the library owns the entity, but only this
// application knows that agents point at MCP servers.
func (s *Service) enrichMCPServerUsage(m *domain.MCPServer) {
	if m == nil || s.Agents == nil {
		return
	}
	for _, a := range s.Agents.List() {
		if !containsString(a.MCPServerIDs, m.ID) {
			continue
		}
		m.AgentCount++
		m.AgentNames = append(m.AgentNames, a.Name)
	}
}

// unlinkMCPServerFromAgents removes a server from every agent referencing it.
// It backs mcpserver.Hooks.BeforeDelete.
func (s *Service) unlinkMCPServerFromAgents(m *domain.MCPServer) error {
	if s.Agents == nil || m == nil {
		return nil
	}
	for _, a := range s.Agents.List() {
		if !containsString(a.MCPServerIDs, m.ID) {
			continue
		}
		filtered := make([]string, 0, len(a.MCPServerIDs))
		for _, id := range a.MCPServerIDs {
			if id != m.ID {
				filtered = append(filtered, id)
			}
		}
		if _, err := s.Agents.Update(a.ID, a.Name, a.Description, a.PromptID, a.SkillIDs, filtered); err != nil {
			return err
		}
	}
	return nil
}
