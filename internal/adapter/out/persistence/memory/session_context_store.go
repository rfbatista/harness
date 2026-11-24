package memory

import (
	"context"
	"encoding/json"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.SessionContextStore = (*SessionContextStore)(nil)

// SessionContextStore holds session contexts in memory, as JSON so a caller
// never shares a stored value.
type SessionContextStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

// NewSessionContextStore returns an empty store.
func NewSessionContextStore() *SessionContextStore {
	return &SessionContextStore{data: map[string][]byte{}}
}

// Load implements ports.SessionContextStore.
func (s *SessionContextStore) Load(_ context.Context, sessionID string) (*domain.SessionContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[sessionID]
	if !ok {
		return nil, nil
	}
	var c domain.SessionContext
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save implements ports.SessionContextStore.
func (s *SessionContextStore) Save(_ context.Context, c *domain.SessionContext) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[c.SessionID] = b
	return nil
}

// Delete implements ports.SessionContextStore.
func (s *SessionContextStore) Delete(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, sessionID)
	return nil
}
