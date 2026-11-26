package ports

import (
	"context"
	"time"
)

// StoredContextMap is a generated context map as the harness holds it.
type StoredContextMap struct {
	RepositoryID string
	// Commit is the default-branch commit the map was generated from.
	Commit      string
	Content     string
	GeneratedAt time.Time
}

// ContextMapStore keeps the generated maps (driven), one per repository.
type ContextMapStore interface {
	// GetContextMap is nil when none was generated.
	GetContextMap(ctx context.Context, repositoryID string) (*StoredContextMap, error)
	PutContextMap(ctx context.Context, m StoredContextMap) error
}

// ContextMapView is a repository's context map as the API and the session
// start read it. Enabled is false when its effective configuration turns the
// map off; Available is false when there is nothing to map yet (a repository
// with no commit on its default branch).
type ContextMapView struct {
	RepositoryID string     `json:"repository_id"`
	Enabled      bool       `json:"enabled"`
	Available    bool       `json:"available"`
	Commit       string     `json:"commit,omitempty"`
	Content      string     `json:"content,omitempty"`
	GeneratedAt  *time.Time `json:"generated_at,omitempty"`
}

// ContextMaps is the context map use cases (*contextmap.Service). The map is
// regenerated when the default branch moved since it was generated. Errors:
// REPOSITORY_NOT_FOUND.
type ContextMaps interface {
	GetContextMap(ctx context.Context, repositoryID string) (*ContextMapView, error)
}

// ContextMapChange says a repository's context map was regenerated; ProjectID
// routes it and is not on the wire.
type ContextMapChange struct {
	RepositoryID string `json:"repository_id"`
	Commit       string `json:"commit"`
	ProjectID    string `json:"-"`
}
