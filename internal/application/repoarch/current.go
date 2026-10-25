package repoarch

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// Current is a repository's saved record as the rule checks read it: compiled
// once per version.
type Current struct {
	RepositoryID string
	ProjectID    string
	Version      int
	Compiled     *domain.CompiledArchitecture
}

// Current is the repository's current saved record, compiled; nil when it has
// none. It is one in-process lookup after the first: a save, a delete or the
// repository's deletion forgets the entry, so rules stay live (ADR 0033).
func (s *Service) Current(ctx context.Context, repositoryID string) (*Current, error) {
	s.cacheMu.RLock()
	cur, ok := s.cache[repositoryID]
	s.cacheMu.RUnlock()
	if ok {
		return cur, nil
	}
	stored, err := s.store.Get(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		rec := stored.Record
		cur = &Current{RepositoryID: repositoryID, ProjectID: stored.ProjectID, Version: stored.Version, Compiled: rec.Compile()}
	}
	s.cacheMu.Lock()
	s.cache[repositoryID] = cur
	s.cacheMu.Unlock()
	return cur, nil
}

// forget drops a repository's cached record; "" drops them all.
func (s *Service) forget(repositoryID string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if repositoryID == "" {
		clear(s.cache)
		return
	}
	delete(s.cache, repositoryID)
}

// Rule events are listed with a default of 50 and at most 500.
const (
	defaultEventLimit = 50
	maxEventLimit     = 500
)

// ListArchitectureRuleEvents implements ports.RepositoryArchitectures.
func (s *Service) ListArchitectureRuleEvents(ctx context.Context, repositoryID string, since time.Time, limit int) ([]domain.ArchitectureRuleEvent, error) {
	if _, err := s.repos.GetRepository(ctx, repositoryID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultEventLimit
	}
	limit = min(limit, maxEventLimit)
	if s.events == nil {
		return []domain.ArchitectureRuleEvent{}, nil
	}
	return s.events.List(ctx, repositoryID, since, limit)
}
