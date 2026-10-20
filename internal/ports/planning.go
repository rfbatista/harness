package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// TicketRepository is the outbound port for persisting and retrieving tickets.
// Tickets are scoped to a project.
type TicketRepository interface {
	Get(ctx context.Context, id string) *domain.Ticket
	// ListByProject lists a project's tickets that are not archived.
	ListByProject(ctx context.Context, projectID string) []*domain.Ticket
	// ListArchivedByProject lists a project's archived tickets, newest
	// archived first.
	ListArchivedByProject(ctx context.Context, projectID string) []*domain.Ticket
	Create(projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	Update(id, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	// SetArchived archives the ticket at at, or restores it when at is nil.
	SetArchived(id string, at *time.Time) (*domain.Ticket, error)
	Delete(id string) error
	// ActivityByProject answers each project's open and archived task
	// counts and newest update, for every project that has tasks.
	ActivityByProject() (map[string]TaskActivity, error)
}

// DocumentRepository is the outbound port for persisting and retrieving documents.
// It also owns the ticket_documents join table via Link/Unlink/ListByTicket/
// ListTicketIDsByDocument. Update keeps the stored format when format is
// empty. ListByProject narrows to one scope when scope is set and lists every
// document when it is "". SetScope moves a document between scopes and bumps
// its updated_at.
type DocumentRepository interface {
	Get(id string) *domain.Document
	ListByProject(projectID string, scope domain.DocumentScope) []*domain.Document
	ListByTicket(ctx context.Context, ticketID string) []*domain.Document
	// ListByTickets lists the documents linked to any of ticketIDs, each once.
	ListByTickets(ctx context.Context, ticketIDs []string) []*domain.Document
	ListTicketIDsByDocument(documentID string) []string
	Create(projectID, title, content string, format domain.DocumentFormat, scope domain.DocumentScope) (*domain.Document, error)
	Update(id, title, content string, format domain.DocumentFormat) (*domain.Document, error)
	// UpdateIfContent is Update as a compare-and-set: it writes only while
	// the stored content is still expectedContent. Otherwise it changes
	// nothing and answers DOCUMENT_CHANGED; a missing document is
	// DOCUMENT_NOT_FOUND.
	UpdateIfContent(id, expectedContent, title, content string, format domain.DocumentFormat) (*domain.Document, error)
	SetScope(id string, scope domain.DocumentScope) (*domain.Document, error)
	Delete(id string) error
	Link(ticketID, documentID string) error
	Unlink(ticketID, documentID string) error
}

// Driving ports of the planning service. *planning.Service satisfies them.

// TicketReader reads a project's tickets: what a page that only shows tasks
// needs. Network-safe like TicketBoard, which embeds it.
type TicketReader interface {
	GetTicket(ctx context.Context, id string) (*domain.Ticket, error)
	ListTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error)
}

// TicketDocumentReader reads the documents linked to a task (written by its
// agents in task/docs of their virtual shell, or linked by a person): what the
// task's documents page needs. Planning implements it.
type TicketDocumentReader interface {
	ListTicketDocuments(ctx context.Context, ticketID string) []*domain.Document
}

// DocumentReader reads what the documents pages show: a task's linked
// documents, the documents of several tasks at once (the reviews inbox), a
// project's documents by scope (the project library lists project-scoped
// ones), and the tasks a document is linked to. Planning implements it.
type DocumentReader interface {
	TicketDocumentReader
	ListTasksDocuments(ctx context.Context, ticketIDs []string) []*domain.Document
	ListDocuments(projectID string, scope domain.DocumentScope) []*domain.Document
	ListDocumentTickets(documentID string) []*domain.Ticket
}

// TicketBoard manages a project's tickets (tasks, in the UI). It is
// network-safe: tui-client implements it over HTTP, so every method takes a
// context and reports failure, including TICKET_NOT_FOUND, as an error.
type TicketBoard interface {
	TicketReader
	CreateTicket(ctx context.Context, projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	UpdateTicket(ctx context.Context, id, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	DeleteTicket(ctx context.Context, id string) error
}

// TicketPatch is a partial change to a ticket. A nil field keeps the stored
// value; a non-nil one replaces it. It is how the kanban board moves a card
// without touching its text, and how an editor saves text without moving it.
type TicketPatch struct {
	Title       *string
	Description *string
	Status      *domain.TicketStatus
	// StatusReason and BySessionID say why and by whom the status moves; an
	// empty BySessionID is a person. They are recorded only when it does.
	StatusReason string
	BySessionID  string
	// ByArchitect says BySessionID is the task's architect at the time of
	// the move, so the history keeps the role it moved it in.
	ByArchitect bool
}

// TicketPatcher applies a partial change to one ticket. A present blank title
// is INVALID_INPUT; a present status must be one of the five values, except
// that an empty one keeps the current status (callers that always send every
// field rely on it). A patch that changes nothing is a no-op.
type TicketPatcher interface {
	PatchTicket(ctx context.Context, id string, patch TicketPatch) (*domain.Ticket, error)
}

// TicketArchiver archives a done ticket and restores it: the person's alone,
// never a session's. Archiving a ticket that is not done is TASK_NOT_DONE;
// archiving an archived one and restoring an unarchived one change nothing
// and answer the ticket. While archived, a ticket cannot change status
// (TASK_ARCHIVED) and leaves ListTickets; ListArchivedTickets lists the
// project's archived tickets, newest archived first. ArchiveDoneTickets
// archives every done ticket of the project that is not archived yet and
// answers their ids, never nil.
type TicketArchiver interface {
	ArchiveTicket(ctx context.Context, id string) (*domain.Ticket, error)
	RestoreTicket(ctx context.Context, id string) (*domain.Ticket, error)
	ListArchivedTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error)
	ArchiveDoneTickets(ctx context.Context, projectID string) ([]string, error)
}

// ArchivedTaskCounter counts a project's archived tasks, for the project
// page's "Archived (N)" link. Planning implements it.
type ArchivedTaskCounter interface {
	ArchivedTaskCount(ctx context.Context, projectID string) (int, error)
}

// TicketAnnouncer puts a ticket change on the project feed, so the board and
// the rail move while the developer watches. The orchestration, which owns
// the feed, implements it; planning calls it after every create, update and
// delete. deleted marks the ticket's last message.
type TicketAnnouncer interface {
	AnnounceTicket(tk *domain.Ticket, deleted bool)
}

// TicketReannouncer puts a task on the project feed again, read and
// decorated as it is now, when one of its derived fields (its merge state)
// changed without the task being written. Planning implements it.
type TicketReannouncer interface {
	ReannounceTicket(ctx context.Context, id string)
}

// MergeStateRefresher computes a task's merge state from git now and keeps
// it, so the next decoration of the task carries it. Planning calls it when a
// task moves into review or done.
type MergeStateRefresher interface {
	Refresh(ctx context.Context, taskID string) (domain.MergeState, error)
}

// DocumentLibrary manages a project's documents and their links to tickets.
// CreateDocument with an empty format writes markdown and with an empty scope
// a project document (what the API means by "created at the project");
// UpdateDocument with an empty format keeps the stored one; any other format
// or scope is INVALID_INPUT. SetDocumentScope moves a document between task
// and project scope without touching its ticket links; the same scope again
// changes nothing. ListDocumentTickets is the tickets a document is linked to.
type DocumentLibrary interface {
	CreateDocument(projectID, title, content string, format domain.DocumentFormat, scope domain.DocumentScope) (*domain.Document, error)
	GetDocument(id string) *domain.Document
	ListDocuments(projectID string, scope domain.DocumentScope) []*domain.Document
	UpdateDocument(id, title, content string, format domain.DocumentFormat) (*domain.Document, error)
	SetDocumentScope(id string, scope domain.DocumentScope) (*domain.Document, error)
	DeleteDocument(id string) error
	LinkDocument(ticketID, documentID string) error
	UnlinkDocument(ticketID, documentID string) error
	ListTicketDocuments(ctx context.Context, ticketID string) []*domain.Document
	ListDocumentTickets(documentID string) []*domain.Ticket
}

// Planning is tickets and documents together.
type Planning interface {
	TicketBoard
	TicketPatcher
	DocumentLibrary
}
