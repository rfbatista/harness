package orchestration

import (
	"log/slog"

	"operators-mcp/internal/domain"
)

// resolveAttachedMCPServers returns the MCP servers a session gets: its
// agent's, then its project's, a server named twice once (ADR 0031). Servers
// are named in the catalog, whose servers' ids are their names. A name no
// server has is skipped and logged: a catalog edited since never fails a
// session. A session with no agent gets its project's only; with neither,
// none.
func resolveAttachedMCPServers(agent *domain.Agent, project *domain.Project, all []*domain.MCPServer) []*domain.MCPServer {
	var names []string
	if agent != nil {
		names = append(names, agent.MCPServerIDs...)
	}
	if project != nil {
		names = append(names, project.MCPServers...)
	}
	byKey := make(map[string]*domain.MCPServer, 2*len(all))
	for _, m := range all {
		byKey[m.ID] = m
		byKey[m.Name] = m
	}
	var out []*domain.MCPServer
	seen := make(map[*domain.MCPServer]bool, len(names))
	for _, n := range names {
		m, ok := byKey[n]
		if !ok {
			slog.Warn("a session's MCP server is not in the agent catalog; skipped", "server", n)
			continue
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// sessionAllowedTools are the tools a session may use without asking: the
// request's, then its agent's, each once. An agent with none leaves Claude's
// default.
func sessionAllowedTools(requested []string, agent *domain.Agent) []string {
	if agent == nil || len(agent.AllowedTools) == 0 {
		return requested
	}
	out := append([]string(nil), requested...)
	for _, t := range agent.AllowedTools {
		found := false
		for _, o := range out {
			if o == t {
				found = true
				break
			}
		}
		if !found {
			out = append(out, t)
		}
	}
	return out
}
