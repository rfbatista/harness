package memory

import (
	"sync"
	"time"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

var _ ports.MCPServerRepository = (*MCPServerStore)(nil)

// MCPServerStore holds in-memory MCP server configs keyed by id.
type MCPServerStore struct {
	mu      sync.RWMutex
	servers map[string]*domain.MCPServer
}

func NewMCPServerStore() *MCPServerStore {
	return &MCPServerStore{servers: make(map[string]*domain.MCPServer)}
}

func (s *MCPServerStore) Get(id string) *domain.MCPServer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.servers[id]
	if !ok {
		return nil
	}
	return cloneMCPServer(m)
}

func (s *MCPServerStore) List() []*domain.MCPServer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.MCPServer, 0, len(s.servers))
	for _, m := range s.servers {
		out = append(out, cloneMCPServer(m))
	}
	return out
}

func (s *MCPServerStore) Create(in domain.MCPServerInput) (*domain.MCPServer, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &domain.MCPServer{
		ID:          id,
		Name:        in.Name,
		Description: in.Description,
		Transport:   in.Transport,
		Command:     in.Command,
		Args:        append([]string(nil), in.Args...),
		URL:         in.URL,
		Env:         cloneMap(in.Env),
		Headers:     cloneMap(in.Headers),
	}
	s.mu.Lock()
	s.servers[id] = m
	s.mu.Unlock()
	return cloneMCPServer(m), nil
}

func (s *MCPServerStore) Update(id string, in domain.MCPServerInput) (*domain.MCPServer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.servers[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "mcp server not found"}
	}
	m.Name = in.Name
	m.Description = in.Description
	m.Transport = in.Transport
	m.Command = in.Command
	m.Args = append([]string(nil), in.Args...)
	m.URL = in.URL
	m.Env = cloneMap(in.Env)
	m.Headers = cloneMap(in.Headers)
	return cloneMCPServer(m), nil
}

func (s *MCPServerStore) UpdateProbeResult(id string, result domain.MCPProbeResult) (*domain.MCPServer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.servers[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "mcp server not found"}
	}
	now := time.Now()
	m.LastProbeAt = &now
	m.LastProbeStatus = result.Status
	m.LastProbeError = result.Error
	m.ToolCount = result.ToolCount
	m.ResourceCount = result.ResourceCount
	m.PromptCount = result.PromptCount
	return cloneMCPServer(m), nil
}

func (s *MCPServerStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.servers[id]; !ok {
		return &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "mcp server not found"}
	}
	delete(s.servers, id)
	return nil
}

func cloneMCPServer(m *domain.MCPServer) *domain.MCPServer {
	if m == nil {
		return nil
	}
	c := *m
	c.Args = append([]string(nil), m.Args...)
	c.Env = cloneMap(m.Env)
	c.Headers = cloneMap(m.Headers)
	return &c
}

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
