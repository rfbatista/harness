package orchestration

import (
	"slices"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
)

// servicesServerName names the built-in services MCP server in a session's
// config, and so its tools: mcp__services__* (ADR 0075). It is not in the MCP
// catalog's file; the harness serves it itself.
const servicesServerName = "services"

// namesServicesServer says whether the session's agent lists the services
// server, or its project's list names it.
func namesServicesServer(agent *domain.Agent, project *domain.Project) bool {
	return agent != nil && slices.Contains(agent.MCPServerIDs, servicesServerName) ||
		project != nil && slices.Contains(project.MCPServers, servicesServerName)
}

// applyServicesServer gives the session the services server with its tools
// allowed, when its agent or project names it and the host serves one. A
// session keeps what it started with.
func (s *Service) applyServicesServer(cfg *llmkit.SessionConfig, sessionID string, agent *domain.Agent, project *domain.Project) {
	if s.ServicesServerURL == nil || !namesServicesServer(agent, project) {
		return
	}
	cfg.MCPServers = append(cfg.MCPServers, llmkit.MCPServerSpec{
		Name:      servicesServerName,
		Transport: "http",
		URL:       s.ServicesServerURL(sessionID),
	})
	for _, name := range tooling.SessionServicesToolNames {
		cfg.AllowedTools = append(cfg.AllowedTools, llmkit.MCPToolRef(servicesServerName, name))
	}
	cfg.AppendSystem = appendSection(cfg.AppendSystem, servicesBrief)
}

// servicesBrief names the services tools in the system prompt, and the two
// have to agree with tooling.SessionServicesToolNames.
const servicesBrief = `## Services

Your project's services are programs the person defined to run beside each other: a command line in the agent sandbox or a container image. You operate them, and you can ask to create or change them. Every change (create, update, start, stop, restart) waits for the person to approve it: the call blocks until they answer, so say why in reason. A denied request fails with the person's note.

- mcp__services__list_services — the project's services with status and last run; environment variables show as names only
- mcp__services__service_status — one service: who started it, where it runs, how it ended
- mcp__services__create_service — request a new command or image service, with its variables
- mcp__services__update_service — request changes to a service's definition, setting or unsetting saved variables
- mcp__services__start_service — request a start and wait until it runs or fails; worktree "own" runs a command service of your repository in your worktree
- mcp__services__stop_service — request a stop, whoever started it
- mcp__services__restart_service — request a stop and start again; each argument you omit repeats the last run
- mcp__services__read_service_logs — the last tail_lines of its log, or from a byte offset, to follow it with next_offset`
