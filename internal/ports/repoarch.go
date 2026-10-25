package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// RepositoryArchitectures is the repository architecture use cases: one
// structured record per repository, saved only by the person, read by
// sessions and hooks. *repoarch.Service satisfies it.
//
// An unknown repository is REPOSITORY_NOT_FOUND.
type RepositoryArchitectures interface {
	// GetRepositoryArchitecture is the stored record, or the starter record
	// (Exists false, version 0) when none is saved.
	GetRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.RepositoryArchitecture, error)
	// SaveRepositoryArchitecture stores record as a new version when
	// baseVersion is the stored version (0 to create), and replaces the
	// repository's layer zones in the same transaction. ARCHITECTURE_INVALID
	// carries the field errors; ARCHITECTURE_CHANGED says the stored version
	// moved.
	SaveRepositoryArchitecture(ctx context.Context, repositoryID string, baseVersion int, record domain.ArchitectureRecord) (*domain.RepositoryArchitecture, error)
	// DeleteRepositoryArchitecture removes the record and its layer zones.
	DeleteRepositoryArchitecture(ctx context.Context, repositoryID string) error
}

// StoredArchitecture is a repository's record row.
type StoredArchitecture struct {
	RepositoryID string
	ProjectID    string
	Version      int
	Record       domain.ArchitectureRecord
	UpdatedAt    time.Time
	// The last export: its version, commit, time and the SHA-256 of the file
	// it wrote.
	ExportedVersion int
	ExportedCommit  string
	ExportedAt      *time.Time
	ExportedSHA     string
}

// ArchitectureRecordStore keeps the records and their layer zones (driven).
type ArchitectureRecordStore interface {
	// Get answers the repository's row, or nil when it has none.
	Get(ctx context.Context, repositoryID string) (*StoredArchitecture, error)
	// Save stores record as the next version, replacing the repository's
	// layer zones with zones, in one transaction. It refuses with
	// ARCHITECTURE_CHANGED when the stored version is not baseVersion (0 when
	// there is none). It answers the new version.
	Save(ctx context.Context, repositoryID, projectID string, baseVersion int, record domain.ArchitectureRecord, zones []*domain.Zone) (*StoredArchitecture, error)
	// Delete removes the record and the repository's layer zones; no error
	// when it has none.
	Delete(ctx context.Context, repositoryID string) error
	// DeleteByProject does the same for every repository of the project.
	DeleteByProject(ctx context.Context, projectID string) error
}
