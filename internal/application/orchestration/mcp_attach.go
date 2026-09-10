package orchestration

import "operators-mcp/internal/domain"

// resolveAttachedMCPServers returns MCP servers explicitly linked to the agent.
// Empty agent.MCPServerIDs means no external MCPs are attached (explicit opt-in).
func resolveAttachedMCPServers(agent *domain.Agent, all []*domain.MCPServer) []*domain.MCPServer {
	if agent == nil || len(agent.MCPServerIDs) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(agent.MCPServerIDs))
	for _, id := range agent.MCPServerIDs {
		allowed[id] = true
	}
	filtered := make([]*domain.MCPServer, 0, len(agent.MCPServerIDs))
	for _, m := range all {
		if allowed[m.ID] {
			filtered = append(filtered, m)
		}
	}
	return filtered
}
