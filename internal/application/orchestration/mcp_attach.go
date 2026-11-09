package orchestration

import (
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
)

// mcpAttachment is what a session gets of the catalog's MCP servers.
type mcpAttachment struct {
	// Servers are the catalog servers to attach, in order, each once.
	Servers []*domain.MCPServer
	// Denied are the tools hidden from the session, as mcp__<server>__<tool>:
	// the tools of an attached server whose effective setting in the project
	// is disabled.
	Denied []string
}

// resolveAttachedMCPServers returns the MCP servers a session gets: its
// agent's, then its project's Added ones, a server named twice once (ADR
// 0031), less every server the project bans (ADR 0067). Servers are named in
// the catalog, whose servers' ids are their names. A name no server has is
// skipped and logged: a catalog edited since never fails a session. A session
// with no agent gets its project's only; with neither, none.
func resolveAttachedMCPServers(agent *domain.Agent, project *domain.Project, all []*domain.MCPServer) []*domain.MCPServer {
	return resolveMCP(agent, project, all, nil).Servers
}

// resolveMCP is the one decision at session start over the agent, the project
// and the catalog: the servers attached and the tools denied. disabledByDefault
// is each server's global tool default, by name. A tool left at Default in the
// project follows it; an override of the project wins. The built-in task,
// browser and codegraph servers are not catalog servers, and the services server
// is built in: none of them is ever banned or trimmed.
func resolveMCP(agent *domain.Agent, project *domain.Project, all []*domain.MCPServer, disabledByDefault map[string][]string) mcpAttachment {
	var names []string
	var banned []string
	if agent != nil {
		names = append(names, agent.MCPServerIDs...)
	}
	if project != nil {
		names = append(names, project.MCPServers...)
		banned = project.MCPBanned
	}
	byKey := make(map[string]*domain.MCPServer, 2*len(all))
	for _, m := range all {
		byKey[m.ID] = m
		byKey[m.Name] = m
	}
	var out mcpAttachment
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if n == servicesServerName {
			continue // built in: the harness serves it itself (applyServicesServer), never banned or trimmed
		}
		if slices.Contains(banned, n) || seen[n] {
			continue
		}
		m, ok := byKey[n]
		if !ok {
			slog.Warn("a session's MCP server is not in the agent catalog; skipped", "server", n)
			continue
		}
		if slices.Contains(banned, m.Name) || seen[m.Name] {
			continue
		}
		seen[n], seen[m.Name] = true, true
		out.Servers = append(out.Servers, m)
		out.Denied = append(out.Denied, deniedTools(domain.Slug(m.Name), m.Name, project, disabledByDefault[m.Name])...)
	}
	return out
}

// deniedTools are one attached server's tools whose effective setting is
// disabled, as mcp__<server>__<tool>. server is the name the session knows
// the server by; the project's overrides are under the catalog name.
func deniedTools(server, catalogName string, project *domain.Project, disabledByDefault []string) []string {
	var overrides map[string]string
	if project != nil {
		overrides = project.MCPToolOverrides[catalogName]
	}
	var denied []string
	for _, tool := range disabledByDefault {
		if overrides[tool] != domain.ToolEnabled && !slices.Contains(denied, tool) {
			denied = append(denied, tool)
		}
	}
	var extra []string
	for tool, v := range overrides {
		if v == domain.ToolDisabled && !slices.Contains(denied, tool) {
			extra = append(extra, tool)
		}
	}
	slices.Sort(extra)
	denied = append(denied, extra...)
	for i, tool := range denied {
		denied[i] = llmkit.MCPToolRef(server, tool)
	}
	return denied
}

// withDeniedMCPTools hides tools from the session through its Claude
// settings' permissions.deny, beside whatever the settings already hold. A
// deny rule wins over an allowed tool, so an agent's allowed tools cannot
// bring one back.
func withDeniedMCPTools(cfg *llmkit.SessionConfig, denied []string) {
	if len(denied) == 0 {
		return
	}
	var dc claude.Config
	switch d := cfg.Driver.(type) {
	case nil:
	case claude.Config:
		dc = d
	default:
		return
	}
	var settings claude.Settings
	if dc.Settings != nil {
		settings = *dc.Settings
	}
	extra := make(map[string]json.RawMessage, len(settings.Extra)+1)
	for k, v := range settings.Extra {
		extra[k] = v
	}
	// permissions may hold other keys (allow, ask…) and deny entries already.
	permissions := map[string]json.RawMessage{}
	if raw, ok := extra["permissions"]; ok {
		_ = json.Unmarshal(raw, &permissions)
	}
	var deny []string
	if raw, ok := permissions["deny"]; ok {
		_ = json.Unmarshal(raw, &deny)
	}
	for _, d := range denied {
		if !slices.Contains(deny, d) {
			deny = append(deny, d)
		}
	}
	permissions["deny"], _ = json.Marshal(deny)
	extra["permissions"], _ = json.Marshal(permissions)
	settings.Extra = extra
	dc.Settings = &settings
	cfg.Driver = dc
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
