package agents

import (
	"context"

	"operators-mcp/internal/domain"
)

// ListAgents returns all agents with resolved prompts, skills and MCP servers.
func (s *Service) ListAgents(_ context.Context) ([]*domain.Agent, error) {
	agents := s.agents.List()
	for _, a := range agents {
		s.ResolveAgentRelations(a)
	}
	return agents, nil
}

// GetAgent returns one agent by id with its relations resolved, or AGENT_NOT_FOUND.
func (s *Service) GetAgent(_ context.Context, id string) (*domain.Agent, error) {
	a := s.agents.Get(id)
	if a == nil {
		return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
	}
	s.ResolveAgentRelations(a)
	return a, nil
}

// CreateAgent creates an agent with the given name, description, prompt ID, skill IDs, and MCP server IDs.
func (s *Service) CreateAgent(_ context.Context, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	a, err := s.agents.Create(name, description, promptID, skillIDs, mcpServerIDs)
	if err != nil {
		return nil, err
	}
	s.ResolveAgentRelations(a)
	return a, nil
}

// UpdateAgent updates an existing agent.
func (s *Service) UpdateAgent(_ context.Context, id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	a, err := s.agents.Update(id, name, description, promptID, skillIDs, mcpServerIDs)
	if err != nil {
		return nil, err
	}
	s.ResolveAgentRelations(a)
	return a, nil
}

// DeleteAgent deletes an agent and announces AgentDeleted, so the zones that
// assign it drop it.
func (s *Service) DeleteAgent(_ context.Context, id string) error {
	if s.agents.Get(id) == nil {
		return &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
	}
	if err := s.agents.Delete(id); err != nil {
		return err
	}
	return s.publish(domain.AgentDeleted{AgentID: id})
}

// ResolveAgentRelations populates read-time prompt, skill, and MCP server data for an agent.
func (s *Service) ResolveAgentRelations(a *domain.Agent) {
	s.ResolveAgentPrompt(a)
	s.resolveSkills(a)
	s.resolveMCPServers(a)
}

// ResolveAgentPrompt populates the Prompt field on an agent by looking up PromptID.
func (s *Service) ResolveAgentPrompt(a *domain.Agent) {
	if a == nil || a.PromptID == "" || s.prompts == nil {
		return
	}
	a.Prompt = s.prompts.Get(a.PromptID)
}

// resolveSkills populates Skills from SkillIDs; ids of skills that no longer
// exist are skipped.
func (s *Service) resolveSkills(a *domain.Agent) {
	if a == nil {
		return
	}
	a.Skills = nil
	if len(a.SkillIDs) == 0 || s.capabilities == nil {
		return
	}
	for _, id := range a.SkillIDs {
		if sk := s.capabilities.GetSkill(id); sk != nil {
			a.Skills = append(a.Skills, *sk)
		}
	}
}

// resolveMCPServers populates MCPServers from MCPServerIDs; ids of servers
// that no longer exist are skipped.
func (s *Service) resolveMCPServers(a *domain.Agent) {
	if a == nil {
		return
	}
	a.MCPServers = nil
	if len(a.MCPServerIDs) == 0 || s.capabilities == nil {
		return
	}
	for _, id := range a.MCPServerIDs {
		if m := s.capabilities.GetMCPServer(id); m != nil {
			a.MCPServers = append(a.MCPServers, *m)
		}
	}
}
