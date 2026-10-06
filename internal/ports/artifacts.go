package ports

import (
	"context"
	"io"
	"time"

	"operators-mcp/internal/domain"
)

// ArtifactFilter narrows artifact listings; the HTTP route requires at least one field.
type ArtifactFilter struct {
	SessionID string
	TicketID  string
}

// ArtifactRepository persists published artifacts. Files are never stored;
// only where they are.
type ArtifactRepository interface {
	// Create stores a new artifact, assigning its ID and timestamps.
	Create(a *domain.Artifact) (*domain.Artifact, error)
	// Update rewrites title, note, kind, mime, size, revision and updated_at.
	Update(a *domain.Artifact) error
	Get(id string) *domain.Artifact
	// FindByTarget is the identity rule: one artifact per (session, path) or (session, url).
	FindByTarget(sessionID, path, url string) *domain.Artifact
	// List returns matches ordered by updated_at descending.
	List(f ArtifactFilter) []*domain.Artifact
	// Delete removes the record; a missing one is ARTIFACT_NOT_FOUND.
	Delete(id string) error
	// DeleteBySession removes every record of a session (the session is being deleted).
	DeleteBySession(sessionID string) error
}

// PublishArtifactRequest is what a session asks to publish. Exactly one of
// Path and URL is set; Kind may be empty (inferred).
type PublishArtifactRequest struct {
	SessionID string
	Path      string
	URL       string
	Title     string
	Kind      string
	Note      string
}

// ArtifactPublisher is the session's side: the task tools call it.
type ArtifactPublisher interface {
	// Publish records (or re-publishes) an artifact and announces it on the
	// session's event stream before returning.
	Publish(ctx context.Context, req PublishArtifactRequest) (*domain.Artifact, error)
	// Unpublish removes the record only, never the file; only the session
	// that published it may (ARTIFACT_NOT_YOURS otherwise).
	Unpublish(ctx context.Context, sessionID, artifactID string) error
	// ListTaskArtifacts is every artifact published on the task, newest first.
	ListTaskArtifacts(ctx context.Context, ticketID string) ([]*domain.Artifact, error)
}

// ArtifactFile is an artifact's bytes, opened for one response. The caller closes Content.
type ArtifactFile struct {
	Content  io.ReadSeekCloser
	Mime     string
	Size     int64
	ModTime  time.Time
	Revision int
}

// ArtifactReader is the browser's side: the HTTP API serves it.
type ArtifactReader interface {
	GetArtifact(ctx context.Context, id string) (*domain.Artifact, error)
	ListArtifacts(ctx context.Context, f ArtifactFilter) ([]*domain.Artifact, error)
	DeleteArtifact(ctx context.Context, id string) error
	// OpenArtifactFile opens the artifact's own file (relpath "") or a file
	// next to it, resolved relative to the artifact's directory and confined
	// to the session worktree. Outside it, under .git, missing, a directory,
	// or a url artifact: ARTIFACT_NOT_FOUND.
	OpenArtifactFile(ctx context.Context, id, relpath string) (*ArtifactFile, error)
}

// Artifacts is the whole artifact surface.
type Artifacts interface {
	ArtifactPublisher
	ArtifactReader
}

// SessionAnnouncer puts a persisted event on a session's stream: sequenced,
// logged, fanned out live. The orchestration implements it; other contexts
// use it to speak on the stream without owning it.
type SessionAnnouncer interface {
	Announce(sessionID string, ev SessionEvent)
}
