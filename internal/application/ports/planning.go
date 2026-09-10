package ports

import "operators-mcp/internal/domain"

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
