package memory

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Ensure Store implements ports.ZoneRepository at compile time.
var _ ports.ZoneRepository = (*Store)(nil)

// Store holds in-memory zones keyed by id.
type Store struct {
	mu    sync.RWMutex
	zones map[string]*domain.Zone
}

// NewStore returns a new in-memory zone store.
func NewStore() *Store {
	return &Store{zones: make(map[string]*domain.Zone)}
}

// Get returns the zone by id, or nil if not found.
func (s *Store) Get(id string) *domain.Zone {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, ok := s.zones[id]
	if !ok {
		return nil
	}
	return cloneZone(z)
}

// ListByProject returns all zones for the given project.
func (s *Store) ListByProject(projectID string) []*domain.Zone {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Zone, 0)
	for _, z := range s.zones {
		if z.ProjectID == projectID {
			out = append(out, cloneZone(z))
		}
	}
	return out
}

// Create creates a zone in the given project and returns it with generated id. Name must be non-empty.
func (s *Store) Create(projectID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error) {
	if name == "" {
		return nil, &domain.StructuredError{Code: "INVALID_NAME", Message: "zone name is required"}
	}
	id, err := genID()
	if err != nil {
		return nil, err
	}
	z := &domain.Zone{
		ID:             id,
		ProjectID:      projectID,
		Name:           name,
		Pattern:        pattern,
		Purpose:        purpose,
		Rules:          clonePrompts(rules),
		AssignedAgents: cloneAgents(agents),
		ExplicitPaths:  nil,
	}
	s.mu.Lock()
	s.zones[id] = z
	s.mu.Unlock()
	return cloneZone(z), nil
}

// Update updates a zone by id. Returns StructuredError if not found or invalid.
func (s *Store) Update(id, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
	}
	if name != "" {
		z.Name = name
	}
	z.Pattern = pattern
	z.Purpose = purpose
	z.Rules = clonePrompts(rules)
	z.AssignedAgents = cloneAgents(agents)
	return cloneZone(z), nil
}

// AssignPath adds path to zone's explicit paths. Returns updated zone or error.
func (s *Store) AssignPath(zoneID, path string) (*domain.Zone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[zoneID]
	if !ok {
		return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
	}
	for _, p := range z.ExplicitPaths {
		if p == path {
			return cloneZone(z), nil
		}
	}
	z.ExplicitPaths = append(z.ExplicitPaths, path)
	return cloneZone(z), nil
}

// UnassignPath removes path from zone's explicit paths. No-op if path was not in the list.
func (s *Store) UnassignPath(zoneID, path string) (*domain.Zone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[zoneID]
	if !ok {
		return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
	}
	filtered := make([]string, 0, len(z.ExplicitPaths))
	for _, p := range z.ExplicitPaths {
		if p != path {
			filtered = append(filtered, p)
		}
	}
	z.ExplicitPaths = filtered
	return cloneZone(z), nil
}

// SetBoundedContext links the zone to a bounded context; an empty
// boundedContextID clears the link.
func (s *Store) SetBoundedContext(zoneID, boundedContextID string) (*domain.Zone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[zoneID]
	if !ok {
		return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
	}
	z.BoundedContextID = boundedContextID
	return cloneZone(z), nil
}

// ClearBoundedContext unlinks every zone pointing at the given bounded context.
func (s *Store) ClearBoundedContext(boundedContextID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, z := range s.zones {
		if z.BoundedContextID == boundedContextID {
			z.BoundedContextID = ""
		}
	}
	return nil
}

// DeleteByProject removes all zones for the given project.
func (s *Store) DeleteByProject(projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, z := range s.zones {
		if z.ProjectID == projectID {
			delete(s.zones, id)
		}
	}
	return nil
}

func cloneZone(z *domain.Zone) *domain.Zone {
	c := *z
	c.Rules = clonePrompts(z.Rules)
	c.ExplicitPaths = append([]string(nil), z.ExplicitPaths...)
	c.AssignedAgents = cloneAgents(z.AssignedAgents)
	return &c
}

func cloneAgents(a []domain.Agent) []domain.Agent {
	if a == nil {
		return nil
	}
	return append([]domain.Agent(nil), a...)
}

func clonePrompts(p []domain.Prompt) []domain.Prompt {
	if p == nil {
		return nil
	}
	return append([]domain.Prompt(nil), p...)
}

func genID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
