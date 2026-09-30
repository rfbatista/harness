package capabilities

import (
	"context"

	"operators-mcp/internal/domain"
)

// The MCP server use cases live in harnesskit/mcpserver; this file delegates to
// them.

// ListMCPServers returns all MCP server configurations with usage counts.
func (s *Service) ListMCPServers() []*domain.MCPServer { return s.mcp.List() }

// GetMCPServer returns one MCP server by id, or nil if not found.
func (s *Service) GetMCPServer(id string) *domain.MCPServer { return s.mcp.Get(id) }

// CreateMCPServer creates an MCP server configuration.
func (s *Service) CreateMCPServer(in domain.MCPServerInput) (*domain.MCPServer, error) {
	return s.mcp.Create(in)
}

// UpdateMCPServer applies a partial change to an MCP server configuration.
func (s *Service) UpdateMCPServer(id string, in domain.MCPServerInput) (*domain.MCPServer, error) {
	return s.mcp.Update(id, in)
}

// UpdateMCPServerProbeResult persists probe metadata for a server.
func (s *Service) UpdateMCPServerProbeResult(id string, result domain.MCPProbeResult) (*domain.MCPServer, error) {
	return s.mcp.UpdateProbeResult(id, result)
}

// ImportMCPServers imports servers from Cursor-style JSON.
func (s *Service) ImportMCPServers(content, onDuplicate string) ([]*domain.MCPServer, error) {
	return s.mcp.Import(content, onDuplicate)
}

// ProbeMCPServer connects to a server and reports what it exposes.
func (s *Service) ProbeMCPServer(ctx context.Context, server domain.MCPServer) (domain.MCPProbeResult, error) {
	return s.mcp.Probe(ctx, server)
}

// DeleteMCPServer deletes an MCP server configuration by id. MCPServerDeleted
// is announced first, so references to it go before it does.
func (s *Service) DeleteMCPServer(id string) error { return s.mcp.Delete(id) }
