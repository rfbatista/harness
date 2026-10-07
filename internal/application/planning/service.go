// Package planning provides ticket and document use-cases: CRUD plus the
// many-to-many link between tickets and documents. It is deliberately kept
// separate from the catalog contexts so the planning domain stays isolated.
package planning

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service satisfies every driving port of this package, checked at compile time.
var (
	_ ports.Planning           = (*Service)(nil)
	_ ports.TaskActivityReader = (*Service)(nil)
)

// Service implements ticket/document use-cases over the outbound ports.
type Service struct {
	tickets   ports.TicketRepository
	documents ports.DocumentRepository
	projects  ports.ProjectRepository

	// Announcer puts every ticket change on the project feed. Nil announces
	// nothing (tests, a server without the feed).
	Announcer ports.TicketAnnouncer
	// StatusHistory records every status move, with who made it and why. Nil
	// records nothing.
	StatusHistory ports.TaskStatusChangeRepository
	// Events announces TicketStatusChanged. Nil announces nothing.
	Events ports.EventPublisher
	// Decorator fills a ticket's derived fields (its architect, its pending
	// reviews) before it is returned or announced. Nil leaves them empty.
	Decorator ports.TicketDecorator

	// ticketMu serialises write-then-announce on tickets, so the feed carries
	// a ticket's changes in the order they were applied.
	ticketMu sync.Mutex
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

// announce tells the feed about tk, if anyone is listening.
func (s *Service) announce(tk *domain.Ticket, deleted bool) {
	if s.Announcer != nil && tk != nil {
		s.Announcer.AnnounceTicket(tk, deleted)
	}
}

// decorate fills the tickets' derived fields, when a decorator is wired.
func (s *Service) decorate(tickets ...*domain.Ticket) {
	if s.Decorator != nil {
		s.Decorator.DecorateTickets(tickets...)
	}
}

// statusMoved records a status move and announces it. A failure to record
// is logged: the move itself already happened.
func (s *Service) statusMoved(ctx context.Context, tk *domain.Ticket, patch ports.TicketPatch) {
	c := domain.TaskStatusChange{
		TaskID: tk.ID, ProjectID: tk.ProjectID, Status: tk.Status, Reason: patch.StatusReason,
		BySessionID: patch.BySessionID, By: domain.ChangedByPerson, At: time.Now(),
	}
	if c.BySessionID != "" {
		c.By = domain.ChangedBySession
	}
	if s.StatusHistory != nil {
		if err := s.StatusHistory.Append(&c); err != nil {
			slog.Warn("record task status change", "ticket", tk.ID, "err", err)
		}
	}
	if s.Events != nil {
		if err := s.Events.Publish(ctx, domain.TicketStatusChanged{Change: c}); err != nil {
			slog.Warn("announce task status change", "ticket", tk.ID, "err", err)
		}
	}
}

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
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	tk, err := s.tickets.Create(projectID, title, description, status)
	if err != nil {
		return nil, err
	}
	s.decorate(tk)
	s.announce(tk, false)
	return tk, nil
}

func (s *Service) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	tk := s.tickets.Get(id)
	if tk == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	s.decorate(tk)
	return tk, nil
}

func (s *Service) ListTickets(_ context.Context, projectID string) ([]*domain.Ticket, error) {
	tickets := s.tickets.ListByProject(projectID)
	s.decorate(tickets...)
	return tickets, nil
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
func (s *Service) PatchTicket(ctx context.Context, id string, patch ports.TicketPatch) (*domain.Ticket, error) {
	if id == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
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
		s.decorate(existing)
		return existing, nil
	}
	tk, err := s.tickets.Update(id, title, description, status)
	if err != nil {
		return nil, err
	}
	s.decorate(tk)
	s.announce(tk, false)
	if status != existing.Status {
		s.statusMoved(ctx, tk, patch)
	}
	return tk, nil
}

// DeleteTicket removes the ticket and announces it with its last known state,
// so followers know which project's board loses the card.
// DeleteTicket deletes a task, announces it on the feed, then publishes
// TicketDeleted (outside ticketMu: its handlers take their own locks) so other
// contexts drop what they kept about the task.
func (s *Service) DeleteTicket(ctx context.Context, id string) error {
	last, err := s.deleteTicket(id)
	if err != nil {
		return err
	}
	if s.Events != nil && last != nil {
		if err := s.Events.Publish(ctx, domain.TicketDeleted{TicketID: last.ID, ProjectID: last.ProjectID}); err != nil {
			slog.Warn("announce task deletion", "ticket", id, "err", err)
		}
	}
	return nil
}

func (s *Service) deleteTicket(id string) (*domain.Ticket, error) {
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	last := s.tickets.Get(id)
	if err := s.tickets.Delete(id); err != nil {
		return nil, err
	}
	s.announce(last, true)
	return last, nil
}

// --- Documents ---

func (s *Service) CreateDocument(projectID, title, content string, format domain.DocumentFormat, scope domain.DocumentScope) (*domain.Document, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	if format == "" {
		format = domain.DocumentFormatMarkdown
	}
	if !format.Valid() {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "format must be markdown or html"}
	}
	if scope == "" {
		scope = domain.DocumentScopeProject
	}
	if !scope.Valid() {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "scope must be task or project"}
	}
	if s.projects.Get(projectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	return s.documents.Create(projectID, title, content, format, scope)
}

func (s *Service) GetDocument(id string) *domain.Document { return s.documents.Get(id) }

// ListDocuments lists a project's documents, one scope or every one. Callers
// validate the scope at their edge; an unknown one matches nothing.
func (s *Service) ListDocuments(projectID string, scope domain.DocumentScope) []*domain.Document {
	return s.documents.ListByProject(projectID, scope)
}

// SetDocumentScope moves a document between task and project scope. The
// ticket links stay. The same scope again is a no-op: the document comes
// back as stored, so updated_at moves only when the scope does.
func (s *Service) SetDocumentScope(id string, scope domain.DocumentScope) (*domain.Document, error) {
	if !scope.Valid() {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "scope must be task or project"}
	}
	existing := s.documents.Get(id)
	if existing == nil {
		return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_FOUND", Message: "document not found"}
	}
	if existing.Scope == scope {
		return existing, nil
	}
	return s.documents.SetScope(id, scope)
}

// UpdateDocument replaces title and content. An empty format keeps the
// stored one, so callers that never learned about formats are unaffected.
func (s *Service) UpdateDocument(id, title, content string, format domain.DocumentFormat) (*domain.Document, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	if format != "" && !format.Valid() {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "format must be markdown or html"}
	}
	return s.documents.Update(id, title, content, format)
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

// ListDocumentTickets is the tickets a document is linked to, as stored; a
// link whose ticket is gone is skipped.
func (s *Service) ListDocumentTickets(documentID string) []*domain.Ticket {
	var out []*domain.Ticket
	for _, id := range s.documents.ListTicketIDsByDocument(documentID) {
		if tk := s.tickets.Get(id); tk != nil {
			out = append(out, tk)
		}
	}
	return out
}

// TaskActivityByProject answers each project's open task count and newest
// task update: what a project summary shows.
func (s *Service) TaskActivityByProject(_ context.Context) (map[string]ports.TaskActivity, error) {
	return s.tickets.ActivityByProject()
}
