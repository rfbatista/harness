package memory

import (
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ToolRepository = (*ToolStore)(nil)

type ToolStore struct {
	mu    sync.RWMutex
	tools map[string]*domain.Tool
}

func NewToolStore() *ToolStore {
	return &ToolStore{tools: make(map[string]*domain.Tool)}
}

func (s *ToolStore) Get(id string) *domain.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[id]
	if !ok {
		return nil
	}
	return cloneTool(t)
}

func (s *ToolStore) List() []*domain.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Tool, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, cloneTool(t))
	}
	return out
}

func (s *ToolStore) Create(name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	t := &domain.Tool{
		ID:          id,
		Name:        name,
		Description: description,
		InputSchema: cloneAnyMap(inputSchema),
		Source:      "user",
	}
	s.mu.Lock()
	s.tools[id] = t
	s.mu.Unlock()
	return cloneTool(t), nil
}

func (s *ToolStore) Update(id, name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tools[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "TOOL_NOT_FOUND", Message: "tool not found"}
	}
	t.Name = name
	t.Description = description
	t.InputSchema = cloneAnyMap(inputSchema)
	return cloneTool(t), nil
}

func (s *ToolStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tools[id]; !ok {
		return &domain.StructuredError{Code: "TOOL_NOT_FOUND", Message: "tool not found"}
	}
	delete(s.tools, id)
	return nil
}

func cloneTool(t *domain.Tool) *domain.Tool {
	if t == nil {
		return nil
	}
	c := *t
	c.InputSchema = cloneAnyMap(t.InputSchema)
	c.Handler = nil
	return &c
}

func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
