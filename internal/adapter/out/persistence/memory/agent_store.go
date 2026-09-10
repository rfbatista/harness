package memory

import (
	"sync"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Ensure AgentStore implements ports.AgentRepository at compile time.
var _ ports.AgentRepository = (*AgentStore)(nil)

// AgentStore holds in-memory agents keyed by id.
type AgentStore struct {
	mu     sync.RWMutex
	agents map[string]*domain.Agent
}

// NewAgentStore returns a new in-memory agent store.
func NewAgentStore() *AgentStore {
	return &AgentStore{agents: make(map[string]*domain.Agent)}
}

// Get returns the agent by id, or nil if not found.
func (s *AgentStore) Get(id string) *domain.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.agents[id]
	if !ok {
		return nil
	}
	return cloneAgent(a)
}

// List returns all agents.
func (s *AgentStore) List() []*domain.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, cloneAgent(a))
	}
	return out
}

// Create creates an agent with generated id.
func (s *AgentStore) Create(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	a := &domain.Agent{
		ID:           id,
		Name:         name,
		Description:  description,
		PromptID:     promptID,
		SkillIDs:     append([]string(nil), skillIDs...),
		MCPServerIDs: append([]string(nil), mcpServerIDs...),
	}
	s.mu.Lock()
	s.agents[id] = a
	s.mu.Unlock()
	return cloneAgent(a), nil
}

// Update updates an agent by id.
func (s *AgentStore) Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
	}
	a.Name = name
	a.Description = description
	a.PromptID = promptID
	a.SkillIDs = append([]string(nil), skillIDs...)
	a.MCPServerIDs = append([]string(nil), mcpServerIDs...)
	return cloneAgent(a), nil
}

// Delete removes an agent by id.
func (s *AgentStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agents[id]; !ok {
		return &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
	}
	delete(s.agents, id)
	return nil
}

func cloneAgent(a *domain.Agent) *domain.Agent {
	if a == nil {
		return nil
	}
	c := *a
	c.SkillIDs = append([]string(nil), a.SkillIDs...)
	c.Skills = append([]domain.Skill(nil), a.Skills...)
	c.MCPServerIDs = append([]string(nil), a.MCPServerIDs...)
	c.MCPServers = append([]domain.MCPServer(nil), a.MCPServers...)
	return &c
}
