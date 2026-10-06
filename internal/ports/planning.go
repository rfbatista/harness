package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// TicketRepository is the outbound port for persisting and retrieving tickets.
// Tickets are scoped to a project.
type TicketRepository interface {
	Get(id string) *domain.Ticket
	ListByProject(projectID string) []*domain.Ticket
	Create(projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	Update(id, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	Delete(id string) error
}

// DocumentRepository is the outbound port for persisting and retrieving documents.
// It also owns the ticket_documents join table via Link/Unlink/ListByTicket.
type DocumentRepository interface {
	Get(id string) *domain.Document
	ListByProject(projectID string) []*domain.Document
	ListByTicket(ticketID string) []*domain.Document
	Create(projectID, title, content string) (*domain.Document, error)
	Update(id, title, content string) (*domain.Document, error)
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
// agents through create_task_document, or linked by a person): what the
// task's documents page needs. Planning implements it.
type TicketDocumentReader interface {
	ListTicketDocuments(ticketID string) []*domain.Document
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
}

// TicketPatcher applies a partial change to one ticket. A present blank title
// is INVALID_INPUT; a present status must be one of the five values, except
// that an empty one keeps the current status (callers that always send every
// field rely on it). A patch that changes nothing is a no-op.
type TicketPatcher interface {
	PatchTicket(ctx context.Context, id string, patch TicketPatch) (*domain.Ticket, error)
}

// DocumentLibrary manages a project's documents and their links to tickets.
type DocumentLibrary interface {
	CreateDocument(projectID, title, content string) (*domain.Document, error)
	GetDocument(id string) *domain.Document
	ListDocuments(projectID string) []*domain.Document
	UpdateDocument(id, title, content string) (*domain.Document, error)
	DeleteDocument(id string) error
	LinkDocument(ticketID, documentID string) error
	UnlinkDocument(ticketID, documentID string) error
	ListTicketDocuments(ticketID string) []*domain.Document
}

// Planning is tickets and documents together.
type Planning interface {
	TicketBoard
	TicketPatcher
	DocumentLibrary
}
