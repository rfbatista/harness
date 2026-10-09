package ports

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"operators-mcp/internal/domain"
)

// A task's resources and comments: what a person adds to a task for context
// (see "Contract: Harness server ↔ Web UI — task resources and comments").
// Planning owns them; *planning.Service satisfies the driving ports.

// --- Driven ports ---

// TicketResourceRepository persists resources. Lists are oldest first.
type TicketResourceRepository interface {
	// Create stores r with a fresh id and both timestamps set.
	Create(r *domain.TicketResource) (*domain.TicketResource, error)
	Get(id string) *domain.TicketResource
	ListByTicket(ticketID string) []*domain.TicketResource
	SetTitle(id, title string) (*domain.TicketResource, error)
	Delete(id string) error
	// DeleteByTicket removes every resource of the task and answers their
	// ids, so their bytes can go too.
	DeleteByTicket(ticketID string) ([]string, error)
}

// TicketCommentRepository persists comments. Lists are oldest first.
type TicketCommentRepository interface {
	Create(c *domain.TicketComment) (*domain.TicketComment, error)
	Get(id string) *domain.TicketComment
	// List is the task's comments created strictly after since (zero: all).
	List(ticketID string, since time.Time) []*domain.TicketComment
	SetBody(id, body string) (*domain.TicketComment, error)
	Delete(id string) error
	DeleteByTicket(ticketID string) error
}

// ErrBlobTooLarge is what Stage answers when the content runs past its cap.
var ErrBlobTooLarge = errors.New("blob over its size limit")

// StagedBlob is uploaded content held aside until its resource row exists.
// Exactly one of Commit or Discard is called.
type StagedBlob interface {
	Size() int64
	// Head is up to the first 512 bytes, to infer a type from.
	Head() []byte
	// Commit files the content under id, where Open and Path find it.
	Commit(id string) error
	Discard()
}

// Blob is stored content opened for reading.
type Blob struct {
	Content io.ReadSeekCloser
	ModTime time.Time
}

// ResourceBlobStore keeps uploaded bytes outside any worktree, under the
// harness's data directory.
type ResourceBlobStore interface {
	// Stage copies r aside. More than max bytes is ErrBlobTooLarge, and
	// nothing is kept.
	Stage(r io.Reader, max int64) (StagedBlob, error)
	Open(id string) (*Blob, error)
	// Path is where id's bytes live on this machine.
	Path(id string) string
	// Delete removes id's bytes; a missing blob is not an error.
	Delete(id string) error
}

// --- Driving ports ---

// ResourceUpload is a file as the person sends it. FileName gives the default
// title; Mime is what the client declared ("" when it did not).
type ResourceUpload struct {
	TicketID string
	Title    string
	FileName string
	Mime     string
	Content  io.Reader
}

// ResourceContent is an upload's bytes with its record. The caller closes
// Content.
type ResourceContent struct {
	Resource *domain.TicketResource
	Content  io.ReadSeekCloser
	ModTime  time.Time
}

// TaskResourceReader reads a task's resources: the task page's first paint,
// the brief and the agent tools. Network-safe.
type TaskResourceReader interface {
	// ListTicketResources is oldest first; TICKET_NOT_FOUND.
	ListTicketResources(ctx context.Context, ticketID string) ([]*domain.TicketResource, error)
}

// TaskResources is the person's side of resources. Network-safe: tui-client
// implements it over HTTP.
type TaskResources interface {
	TaskResourceReader
	// UploadTicketResource stores a file: TICKET_NOT_FOUND, INVALID_INPUT for
	// an empty file or a bad title, RESOURCE_TOO_LARGE over
	// domain.MaxResourceBytes.
	UploadTicketResource(ctx context.Context, up ResourceUpload) (*domain.TicketResource, error)
	// AddTicketLink stores a link; title "" defaults to the URL.
	AddTicketLink(ctx context.Context, ticketID, url, title string) (*domain.TicketResource, error)
	// RetitleTicketResource renames one; a blank title is INVALID_INPUT.
	RetitleTicketResource(ctx context.Context, id, title string) (*domain.TicketResource, error)
	// DeleteTicketResource removes one and its bytes; RESOURCE_NOT_FOUND.
	DeleteTicketResource(ctx context.Context, id string) error
	// OpenTicketResource opens an upload's bytes; a link is RESOURCE_NOT_FOUND.
	OpenTicketResource(ctx context.Context, id string) (*ResourceContent, error)
}

// TaskResourceFiles reaches an upload's bytes on this machine: what the
// agent tools hand a session. Not network-safe.
type TaskResourceFiles interface {
	TaskResourceReader
	// GetTicketResource is RESOURCE_NOT_FOUND when there is none.
	GetTicketResource(ctx context.Context, id string) (*domain.TicketResource, error)
	OpenTicketResource(ctx context.Context, id string) (*ResourceContent, error)
	// TicketResourcePath is the absolute path of an upload's bytes;
	// RESOURCE_NOT_FOUND for a link or a missing id.
	TicketResourcePath(ctx context.Context, id string) (string, error)
}

// TaskCommentReader reads a task's comments. Network-safe.
type TaskCommentReader interface {
	// ListTicketComments is oldest first, those created strictly after since
	// (zero: all); TICKET_NOT_FOUND.
	ListTicketComments(ctx context.Context, ticketID string, since time.Time) ([]*domain.TicketComment, error)
}

// TaskComments is the person's side of comments. Network-safe.
type TaskComments interface {
	TaskCommentReader
	// AddTicketComment: TICKET_NOT_FOUND, INVALID_INPUT for an empty or
	// oversized body.
	AddTicketComment(ctx context.Context, ticketID, body string) (*domain.TicketComment, error)
	// UpdateTicketComment: COMMENT_NOT_FOUND, INVALID_INPUT. The same body
	// again changes nothing, updated_at included.
	UpdateTicketComment(ctx context.Context, id, body string) (*domain.TicketComment, error)
	DeleteTicketComment(ctx context.Context, id string) error
}

// --- Feed ---

// itemRef is what the feed keeps of a deleted resource or comment.
type itemRef struct {
	ID        string `json:"id"`
	TicketID  string `json:"ticket_id"`
	ProjectID string `json:"project_id"`
}

// TicketResourceChange is a resource as the project feed carries it: the
// whole record after a change, or once deleted only its ids.
type TicketResourceChange struct {
	Resource *domain.TicketResource
	removed  bool
}

// TicketResourceUpdated is the feed change for a resource added or retitled.
func TicketResourceUpdated(r *domain.TicketResource) ProjectChange {
	return ProjectChange{TicketResource: &TicketResourceChange{Resource: r}}
}

// TicketResourceRemoved is the feed change for a deleted resource.
func TicketResourceRemoved(r *domain.TicketResource) ProjectChange {
	return ProjectChange{TicketResource: &TicketResourceChange{Resource: r, removed: true}, Deleted: true}
}

func (c TicketResourceChange) MarshalJSON() ([]byte, error) {
	if c.removed {
		return json.Marshal(itemRef{c.Resource.ID, c.Resource.TicketID, c.Resource.ProjectID})
	}
	return json.Marshal(c.Resource)
}

// UnmarshalJSON reads either form; whether it was a deletion is the change's
// deleted flag.
func (c *TicketResourceChange) UnmarshalJSON(b []byte) error {
	var r domain.TicketResource
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*c = TicketResourceChange{Resource: &r}
	return nil
}

// TicketCommentChange is a comment as the project feed carries it.
type TicketCommentChange struct {
	Comment *domain.TicketComment
	removed bool
}

// TicketCommentUpdated is the feed change for a comment added or edited.
func TicketCommentUpdated(c *domain.TicketComment) ProjectChange {
	return ProjectChange{TicketComment: &TicketCommentChange{Comment: c}}
}

// TicketCommentRemoved is the feed change for a deleted comment.
func TicketCommentRemoved(c *domain.TicketComment) ProjectChange {
	return ProjectChange{TicketComment: &TicketCommentChange{Comment: c, removed: true}, Deleted: true}
}

func (c TicketCommentChange) MarshalJSON() ([]byte, error) {
	if c.removed {
		return json.Marshal(itemRef{c.Comment.ID, c.Comment.TicketID, c.Comment.ProjectID})
	}
	return json.Marshal(c.Comment)
}

func (c *TicketCommentChange) UnmarshalJSON(b []byte) error {
	var m domain.TicketComment
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*c = TicketCommentChange{Comment: &m}
	return nil
}
