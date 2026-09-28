package memory

import (
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.PromptRepository = (*PromptStore)(nil)

// PromptStore holds in-memory prompts keyed by id.
type PromptStore struct {
	mu      sync.RWMutex
	prompts map[string]*domain.Prompt
}

// NewPromptStore returns a new in-memory prompt store.
func NewPromptStore() *PromptStore {
	return &PromptStore{prompts: make(map[string]*domain.Prompt)}
}

func (s *PromptStore) Get(id string) *domain.Prompt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prompts[id]
	if !ok {
		return nil
	}
	return clonePrompt(p)
}

func (s *PromptStore) List() []*domain.Prompt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Prompt, 0, len(s.prompts))
	for _, p := range s.prompts {
		out = append(out, clonePrompt(p))
	}
	return out
}

func (s *PromptStore) Create(name, description, content string) (*domain.Prompt, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	p := &domain.Prompt{ID: id, Name: name, Description: description, Content: content}
	s.mu.Lock()
	s.prompts[id] = p
	s.mu.Unlock()
	return clonePrompt(p), nil
}

func (s *PromptStore) Update(id, name, description, content string) (*domain.Prompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "PROMPT_NOT_FOUND", Message: "prompt not found"}
	}
	p.Name = name
	p.Description = description
	p.Content = content
	return clonePrompt(p), nil
}

func (s *PromptStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.prompts[id]; !ok {
		return &domain.StructuredError{Code: "PROMPT_NOT_FOUND", Message: "prompt not found"}
	}
	delete(s.prompts, id)
	return nil
}

func clonePrompt(p *domain.Prompt) *domain.Prompt {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}
