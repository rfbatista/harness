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

// TicketBoard manages a project's tickets (tasks, in the UI). It is
// network-safe: tui-client implements it over HTTP, so every method takes a
// context and reports failure, including TICKET_NOT_FOUND, as an error.
type TicketBoard interface {
	CreateTicket(ctx context.Context, projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	GetTicket(ctx context.Context, id string) (*domain.Ticket, error)
	ListTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error)
	UpdateTicket(ctx context.Context, id, title, description string, status domain.TicketStatus) (*domain.Ticket, error)
	DeleteTicket(ctx context.Context, id string) error
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
	DocumentLibrary
}
