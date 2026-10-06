// Package planning provides ticket and document use-cases: CRUD plus the
// many-to-many link between tickets and documents. It is deliberately kept
// separate from the catalog contexts so the planning domain stays isolated.
package planning

import (
	"context"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service satisfies every driving port of this package, checked at compile time.
var _ ports.Planning = (*Service)(nil)

// Service implements ticket/document use-cases over the outbound ports.
type Service struct {
	tickets   ports.TicketRepository
	documents ports.DocumentRepository
	projects  ports.ProjectRepository
}

// NewService returns a planning service. projects is used to validate that
// tickets/documents reference an existing project and share a project on link.
func NewService(tickets ports.TicketRepository, documents ports.DocumentRepository, projects ports.ProjectRepository) *Service {
	return &Service{tickets: tickets, documents: documents, projects: projects}
}

func validTicketStatus(s domain.TicketStatus) bool {
	switch s {
	case domain.TicketStatusBacklog, domain.TicketStatusTodo, domain.TicketStatusInProgress, domain.TicketStatusReview, domain.TicketStatusDone:
		return true
	}
	return false
}

// --- Tickets ---

func (s *Service) CreateTicket(_ context.Context, projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	if s.projects.Get(projectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	if status == "" {
		status = domain.TicketStatusBacklog
	}
	if !validTicketStatus(status) {
		return nil, &domain.StructuredError{Code: "INVALID_STATUS", Message: "invalid ticket status"}
	}
	return s.tickets.Create(projectID, title, description, status)
}

func (s *Service) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	tk := s.tickets.Get(id)
	if tk == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	return tk, nil
}

func (s *Service) ListTickets(_ context.Context, projectID string) ([]*domain.Ticket, error) {
	return s.tickets.ListByProject(projectID), nil
}

// UpdateTicket replaces every field. It is the patch with every field present,
// kept for the callers that always send all of them (the TUI client, the
// browser gateway); an empty status keeps the current one, as it always did.
func (s *Service) UpdateTicket(ctx context.Context, id, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	return s.PatchTicket(ctx, id, ports.TicketPatch{Title: &title, Description: &description, Status: &status})
}

// PatchTicket changes only the fields patch carries. It reads the ticket,
// applies the patch, validates the result and writes it back whole, since the
// repository replaces every column. An unchanged ticket is not written.
func (s *Service) PatchTicket(_ context.Context, id string, patch ports.TicketPatch) (*domain.Ticket, error) {
	if id == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	existing := s.tickets.Get(id)
	if existing == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	title, description, status := existing.Title, existing.Description, existing.Status
	if patch.Title != nil {
		if strings.TrimSpace(*patch.Title) == "" {
			return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
		}
		title = *patch.Title
	}
	if patch.Description != nil {
		description = *patch.Description
	}
	if patch.Status != nil && *patch.Status != "" {
		if !validTicketStatus(*patch.Status) {
			return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "status must be one of backlog, todo, in_progress, review, done"}
		}
		status = *patch.Status
	}
	if title == existing.Title && description == existing.Description && status == existing.Status {
		return existing, nil
	}
	return s.tickets.Update(id, title, description, status)
}

func (s *Service) DeleteTicket(_ context.Context, id string) error { return s.tickets.Delete(id) }

// --- Documents ---

func (s *Service) CreateDocument(projectID, title, content string) (*domain.Document, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	if s.projects.Get(projectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	return s.documents.Create(projectID, title, content)
}

func (s *Service) GetDocument(id string) *domain.Document { return s.documents.Get(id) }

func (s *Service) ListDocuments(projectID string) []*domain.Document {
	return s.documents.ListByProject(projectID)
}

func (s *Service) UpdateDocument(id, title, content string) (*domain.Document, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	return s.documents.Update(id, title, content)
}

func (s *Service) DeleteDocument(id string) error { return s.documents.Delete(id) }

// --- Links ---

func (s *Service) LinkDocument(ticketID, documentID string) error {
	tk := s.tickets.Get(ticketID)
	if tk == nil {
		return &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	doc := s.documents.Get(documentID)
	if doc == nil {
		return &domain.StructuredError{Code: "DOCUMENT_NOT_FOUND", Message: "document not found"}
	}
	if tk.ProjectID != doc.ProjectID {
		return &domain.StructuredError{Code: "CROSS_PROJECT_ACCESS", Message: "ticket and document belong to different projects"}
	}
	return s.documents.Link(ticketID, documentID)
}

func (s *Service) UnlinkDocument(ticketID, documentID string) error {
	return s.documents.Unlink(ticketID, documentID)
}

func (s *Service) ListTicketDocuments(ticketID string) []*domain.Document {
	return s.documents.ListByTicket(ticketID)
}
