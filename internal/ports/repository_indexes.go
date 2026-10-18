package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// Repository indexes, in the workspaces context: the codegraph index each
// repository keeps in its own checkout (ADR 0014).
// *workspaces.RepositoryIndexes satisfies the driving port.

// RepositoryIndexes lists repository indexes and rebuilds one (driving).
type RepositoryIndexes interface {
	// ListRepositoryIndexes answers every repository that has an index, has
	// one building or failing, or has codegraph on at any level.
	ListRepositoryIndexes(ctx context.Context) ([]domain.RepositoryIndex, error)
	// RebuildRepositoryIndex starts a build in the background and answers
	// the index as it now stands. It is TOKEN_SAVER_NOT_INSTALLED without
	// codegraph, REPOSITORY_NOT_FOUND or INVALID_ROOT for the repository.
	RebuildRepositoryIndex(ctx context.Context, repositoryID string) (*domain.RepositoryIndex, error)
}

// RepositoryIndexBuilds says whether a build is writing a checkout's index
// (driving): a session start does not copy an index while it may be half
// written.
type RepositoryIndexBuilds interface {
	IndexBuilding(checkout string) bool
}

// RepositoryIndexer builds codegraph indexes in checkouts (driven).
type RepositoryIndexer interface {
	// BuildIndex keeps .codegraph/ out of git through the repository's
	// info/exclude, then runs codegraph (the installed executable) in
	// checkout: a first build, or a full rebuild of the index there.
	BuildIndex(ctx context.Context, codegraph, checkout string) error
	// IndexBuiltAt is when checkout's index was last written; false when it
	// has none.
	IndexBuiltAt(checkout string) (time.Time, bool)
}

// TokenSaverActiveRepositories says where a token saver is on (driving, of
// the capabilities context).
type TokenSaverActiveRepositories interface {
	// RepositoriesWithTokenSaverOn answers the repositories of projectID (of
	// every project when empty) for which saver resolves on at some level:
	// for their sessions without a task, or for a task of their project.
	RepositoriesWithTokenSaverOn(ctx context.Context, saver domain.TokenSaver, projectID string) ([]string, error)
}
