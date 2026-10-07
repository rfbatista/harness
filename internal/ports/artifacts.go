package ports

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"operators-mcp/internal/domain"
)

// ArtifactFilter narrows artifact listings; the HTTP route requires at least
// one of SessionID, TicketID and ProjectID. Scope narrows any of them; under
// task, artifacts that predate scopes count too.
type ArtifactFilter struct {
	SessionID string
	TicketID  string
	ProjectID string
	Scope     domain.ArtifactScope
}

// ArtifactRepository persists published artifacts. Files are never stored
// here; only where they are, and whether the harness holds a copy.
type ArtifactRepository interface {
	// Create stores a new artifact, assigning its ID and timestamps.
	Create(a *domain.Artifact) (*domain.Artifact, error)
	// Update rewrites title, note, kind, mime, size, revision, snapshot and
	// updated_at. Never the scope.
	Update(a *domain.Artifact) error
	Get(id string) *domain.Artifact
	// FindByTarget is the identity rule: one artifact per (session, path) or (session, url).
	FindByTarget(sessionID, path, url string) *domain.Artifact
	// List returns matches ordered by updated_at descending.
	List(f ArtifactFilter) []*domain.Artifact
	// Delete removes the record; a missing one is ARTIFACT_NOT_FOUND.
	Delete(id string) error
	// SetScope moves an artifact between scopes, records whether the harness
	// holds a copy of its bytes, and bumps updated_at. A missing one is
	// ARTIFACT_NOT_FOUND.
	SetScope(id string, scope domain.ArtifactScope, snapshot bool) (*domain.Artifact, error)
	// DeleteTaskScopedBySession removes a session's task-scoped records (the
	// session is being deleted) and returns their ids; project ones stay.
	DeleteTaskScopedBySession(sessionID string) ([]string, error)
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
	// Unpublish removes the record, never the worktree file; only the
	// session that published it may (ARTIFACT_NOT_YOURS otherwise), and not
	// while the project keeps it (ARTIFACT_IN_PROJECT: move it back to task
	// first, or a person deletes it).
	Unpublish(ctx context.Context, sessionID, artifactID string) error
	// ListTaskArtifacts is every artifact published on the task, in either
	// scope, newest first.
	ListTaskArtifacts(ctx context.Context, ticketID string) ([]*domain.Artifact, error)
	// SetArtifactScope moves an artifact between task and project scope.
	// Moving to project snapshots its directory into harness-owned storage;
	// a url artifact, or one whose bytes cannot be copied, is
	// ARTIFACT_NOT_PROMOTABLE. Moving back keeps the copy. The same scope
	// again changes nothing. A move is announced on the producing session's
	// stream as an "artifact" event, while that session exists.
	SetArtifactScope(ctx context.Context, artifactID string, scope domain.ArtifactScope) (*domain.Artifact, error)
	// ListProjectArtifacts is the project's project-scoped artifacts, newest first.
	ListProjectArtifacts(ctx context.Context, projectID string) ([]*domain.Artifact, error)
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
	// next to it, resolved relative to the artifact's directory: from the
	// harness's copy when it holds one (confined to that copy), otherwise
	// from the session worktree (confined to it). Outside, under .git,
	// missing, a directory, or a url artifact: ARTIFACT_NOT_FOUND.
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

// ArtifactChange is an artifact as the project feed carries it: the whole
// record after a change, or, once deleted, only the ids it had (id,
// project_id, ticket_id, attached_ticket_ids), so every page that showed it
// knows to drop it.
type ArtifactChange struct {
	Artifact *domain.Artifact
	removed  bool
}

// ArtifactUpdated is the feed change for an artifact as it is now.
func ArtifactUpdated(a *domain.Artifact) ProjectChange {
	return ProjectChange{Artifact: &ArtifactChange{Artifact: a}}
}

// ArtifactRemoved is the feed change for an artifact that was deleted; a is
// the record as it was before.
func ArtifactRemoved(a *domain.Artifact) ProjectChange {
	return ProjectChange{Artifact: &ArtifactChange{Artifact: a, removed: true}, Deleted: true}
}

func (c ArtifactChange) MarshalJSON() ([]byte, error) {
	if !c.removed {
		return json.Marshal(c.Artifact)
	}
	ids := c.Artifact.AttachedTicketIDs
	if ids == nil {
		ids = []string{}
	}
	return json.Marshal(struct {
		ID                string   `json:"id"`
		ProjectID         string   `json:"project_id"`
		TicketID          string   `json:"ticket_id"`
		AttachedTicketIDs []string `json:"attached_ticket_ids"`
	}{c.Artifact.ID, c.Artifact.ProjectID, c.Artifact.TicketID, ids})
}

// UnmarshalJSON reads either form; whether it was a deletion is the
// change's deleted flag.
func (c *ArtifactChange) UnmarshalJSON(b []byte) error {
	var a domain.Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*c = ArtifactChange{Artifact: &a}
	return nil
}
