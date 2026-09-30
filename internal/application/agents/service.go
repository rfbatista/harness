// Package agents is the agents bounded context: agent templates and the
// prompts they are built from. An agent references capabilities (skills, MCP
// servers) by id; it reads them through the CapabilityReader port and drops a
// reference when the capability announces it is gone.
package agents

import (
	"context"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Agents       = (*Service)(nil)
	_ ports.PromptReader = (*Service)(nil)
)

// Service implements the agents use cases.
type Service struct {
	agents       ports.AgentRepository
	prompts      ports.PromptRepository
	capabilities ports.CapabilityReader // nil: relations resolve to nothing
	events       ports.EventPublisher   // nil: deletes are not announced
}

// NewService returns the agents context.
func NewService(agents ports.AgentRepository, prompts ports.PromptRepository, capabilities ports.CapabilityReader, events ports.EventPublisher) *Service {
	return &Service{agents: agents, prompts: prompts, capabilities: capabilities, events: events}
}

// Subscribe registers the context's reactions to other contexts' events: a
// deleted skill or MCP server is dropped from every agent that references it.
// Returning an error cancels the capability's delete.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.SkillDeleted) error {
		return s.unlink(func(a *domain.Agent) bool {
			if !slices.Contains(a.SkillIDs, ev.SkillID) {
				return false
			}
			a.SkillIDs = without(a.SkillIDs, ev.SkillID)
			return true
		})
	})
	ports.On(sub, func(_ context.Context, ev domain.MCPServerDeleted) error {
		return s.unlink(func(a *domain.Agent) bool {
			if !slices.Contains(a.MCPServerIDs, ev.ServerID) {
				return false
			}
			a.MCPServerIDs = without(a.MCPServerIDs, ev.ServerID)
			return true
		})
	})
}

// unlink applies drop to every agent and saves the ones it changed.
func (s *Service) unlink(drop func(a *domain.Agent) bool) error {
	if s.agents == nil {
		return nil
	}
	for _, a := range s.agents.List() {
		if !drop(a) {
			continue
		}
		if _, err := s.agents.Update(a.ID, a.Name, a.Description, a.PromptID, a.SkillIDs, a.MCPServerIDs); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) publish(ev domain.Event) error {
	if s.events == nil {
		return nil
	}
	return s.events.Publish(context.Background(), ev)
}

// AgentsUsingMCPServer names the agents that reference an MCP server, for the
// capabilities context's usage read model.
func (s *Service) AgentsUsingMCPServer(serverID string) []string {
	if s.agents == nil {
		return nil
	}
	var names []string
	for _, a := range s.agents.List() {
		if slices.Contains(a.MCPServerIDs, serverID) {
			names = append(names, a.Name)
		}
	}
	return names
}

func without(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}
