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

	"github.com/rfbatista/harnesskit/tool"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service satisfies every driving port of this package, checked at compile time.
var (
	_ ports.Planning           = (*Service)(nil)
	_ ports.TaskActivityReader = (*Service)(nil)
	_ ports.TicketArchiver     = (*Service)(nil)
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
	// Events announces TicketStatusChanged, a person's additions to a task,
	// and every document change (DocumentChanged). Nil announces nothing.
	Events ports.EventPublisher
	// Decorator fills a ticket's derived fields (its architect, its pending
	// reviews) before it is returned or announced. Nil leaves them empty.
	Decorator ports.TicketDecorator

	// Resources, Comments and Blobs keep what a person adds to a task. Nil
	// Resources or Blobs leaves resources unavailable; nil Comments, comments.
	Resources ports.TicketResourceRepository
	Comments  ports.TicketCommentRepository
	Blobs     ports.ResourceBlobStore
	// Feed announces resource and comment changes on the project feed. Nil
	// announces nothing.
	Feed ports.ProjectChangeSink
	// ContentURL names where an upload's bytes are served; the route belongs
	// to the HTTP adapter. Nil leaves content_url empty.
	ContentURL func(resourceID string) string
	// MaxResourceBytes caps an upload; zero means domain.MaxResourceBytes.
	MaxResourceBytes int64

	// ticketMu serialises write-then-announce on tickets, so the feed carries
	// a ticket's changes in the order they were applied.
	ticketMu sync.Mutex
	// resourceMu does the same for resources and comments, and keeps their
	// additions' events in the order they were made.
	resourceMu sync.Mutex
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

// decorate fills the tickets' derived fields: their last move and
// status_since from the status history, then what the decorator, when wired,
// knows.
func (s *Service) decorate(ctx context.Context, tickets ...*domain.Ticket) {
	s.fillLastMoves(ctx, tickets)
	if s.Decorator != nil {
		s.Decorator.DecorateTickets(ctx, tickets...)
	}
}

// fillLastMoves sets each ticket's last move and status_since from its most
// recent status change, read in one query. A ticket never moved keeps a nil
// last move and has been in its status since it was created.
func (s *Service) fillLastMoves(ctx context.Context, tickets []*domain.Ticket) {
	if s.StatusHistory == nil {
		return
	}
	ids := make([]string, 0, len(tickets))
	for _, tk := range tickets {
		if tk != nil {
			ids = append(ids, tk.ID)
		}
	}
	latest := s.StatusHistory.LatestByTasks(ctx, ids)
	for _, tk := range tickets {
		if tk == nil {
			continue
		}
		tk.LastMove, tk.StatusSince = nil, tk.CreatedAt
		if c := latest[tk.ID]; c != nil {
			tk.LastMove = &domain.LastMove{Status: c.Status, By: c.By, SessionID: c.BySessionID, Reason: c.Reason, At: c.At}
			tk.StatusSince = c.At
		}
	}
}

// recordMove records a status move in the history, with who made it and why.
// A failure to record is logged: the move itself already happened.
func (s *Service) recordMove(tk *domain.Ticket, patch ports.TicketPatch) domain.TaskStatusChange {
	c := domain.TaskStatusChange{
		TaskID: tk.ID, ProjectID: tk.ProjectID, Status: tk.Status, Reason: patch.StatusReason,
		BySessionID: patch.BySessionID, By: domain.ChangedByPerson, At: time.Now(),
	}
	switch {
	case c.BySessionID != "" && patch.ByArchitect:
		c.By = domain.ChangedByArchitect
	case c.BySessionID != "":
		c.By = domain.ChangedBySession
	}
	if s.StatusHistory != nil {
		if err := s.StatusHistory.Append(&c); err != nil {
			slog.Warn("record task status change", "ticket", tk.ID, "err", err)
		}
	}
	return c
}

// statusMoved announces a recorded status move as TicketStatusChanged.
func (s *Service) statusMoved(ctx context.Context, tk *domain.Ticket, c domain.TaskStatusChange) {
	if s.Events != nil {
		if err := s.Events.Publish(ctx, domain.TicketStatusChanged{Change: c}); err != nil {
			slog.Warn("announce task status change", "ticket", tk.ID, "err", err)
		}
	}
}

// bySession reports whether a call comes from an agent session: one named by
// the patch, or one whose tool call tagged the context. Anything else is the
// person.
func bySession(ctx context.Context, bySessionID string) bool {
	return bySessionID != "" || tool.SessionIDFrom(ctx) != ""
}

// CreateTicket creates a task. A session cannot create one in done: only the
// person moves a task there.
func (s *Service) CreateTicket(ctx context.Context, projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	if s.projects.Get(ctx, projectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	if status == "" {
		status = domain.TicketStatusBacklog
	}
	if !validTicketStatus(status) {
		return nil, &domain.StructuredError{Code: "INVALID_STATUS", Message: "invalid ticket status"}
	}
	if status == domain.TicketStatusDone && bySession(ctx, "") {
		return nil, domain.ErrTaskDoneIsThePersons()
	}
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	tk, err := s.tickets.Create(projectID, title, description, status)
	if err != nil {
		return nil, err
	}
	// The write is done: the change goes out whole even if the request ends.
	s.decorate(context.WithoutCancel(ctx), tk)
	s.announce(tk, false)
	return tk, nil
}

func (s *Service) GetTicket(ctx context.Context, id string) (*domain.Ticket, error) {
	tk := s.tickets.Get(ctx, id)
	if tk == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	s.decorate(ctx, tk)
	return tk, nil
}

// ListTickets lists a project's tickets that are not archived.
func (s *Service) ListTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error) {
	tickets := s.tickets.ListByProject(ctx, projectID)
	s.decorate(ctx, tickets...)
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
// repository replaces every column. An unchanged ticket is not written. A
// session's patch into done is TASK_DONE_IS_THE_PERSONS and changes nothing.
func (s *Service) PatchTicket(ctx context.Context, id string, patch ports.TicketPatch) (*domain.Ticket, error) {
	if id == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	// Refused before anything is read or written: no other field of the call applies.
	if patch.Status != nil && *patch.Status == domain.TicketStatusDone && bySession(ctx, patch.BySessionID) {
		return nil, domain.ErrTaskDoneIsThePersons()
	}
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	existing := s.tickets.Get(ctx, id)
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
	if status != existing.Status && existing.ArchivedAt != nil {
		return nil, &domain.StructuredError{Code: "TASK_ARCHIVED", Message: "the task is archived; restore it before changing its status"}
	}
	if title == existing.Title && description == existing.Description && status == existing.Status {
		s.decorate(ctx, existing)
		return existing, nil
	}
	tk, err := s.tickets.Update(id, title, description, status)
	if err != nil {
		return nil, err
	}
	// The move is recorded before the ticket is decorated, so what is
	// announced and answered carries it as its last move.
	var move *domain.TaskStatusChange
	if status != existing.Status {
		c := s.recordMove(tk, patch)
		move = &c
	}
	// The write is done: the change goes out whole even if the request ends.
	s.decorate(context.WithoutCancel(ctx), tk)
	s.announce(tk, false)
	if move != nil {
		s.statusMoved(ctx, tk, *move)
	}
	return tk, nil
}

// ListArchivedTickets lists a project's archived tickets, newest archived
// first.
func (s *Service) ListArchivedTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error) {
	tickets := s.tickets.ListArchivedByProject(ctx, projectID)
	s.decorate(ctx, tickets...)
	return tickets, nil
}

// ArchiveTicket takes a done ticket off the board, keeping everything it
// has. A ticket that is not done is TASK_NOT_DONE; an archived one is
// answered as it is.
func (s *Service) ArchiveTicket(ctx context.Context, id string) (*domain.Ticket, error) {
	return s.setArchived(ctx, id, true)
}

// RestoreTicket puts an archived ticket back on the board, still done. A
// ticket that is not archived is answered as it is.
func (s *Service) RestoreTicket(ctx context.Context, id string) (*domain.Ticket, error) {
	return s.setArchived(ctx, id, false)
}

func (s *Service) setArchived(ctx context.Context, id string, archive bool) (*domain.Ticket, error) {
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	existing := s.tickets.Get(ctx, id)
	if existing == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	if (existing.ArchivedAt != nil) == archive {
		s.decorate(ctx, existing)
		return existing, nil
	}
	var at *time.Time
	if archive {
		if existing.Status != domain.TicketStatusDone {
			return nil, &domain.StructuredError{Code: "TASK_NOT_DONE", Message: "only a done task can be archived"}
		}
		now := time.Now()
		at = &now
	}
	tk, err := s.tickets.SetArchived(id, at)
	if err != nil {
		return nil, err
	}
	// The write is done: the change goes out whole even if the request ends.
	s.decorate(context.WithoutCancel(ctx), tk)
	s.announce(tk, false)
	return tk, nil
}

// DeleteTicket removes the ticket and announces it with its last known state,
// so followers know which project's board loses the card.
// DeleteTicket deletes a task, announces it on the feed, then publishes
// TicketDeleted (outside ticketMu: its handlers take their own locks) so other
// contexts drop what they kept about the task.
func (s *Service) DeleteTicket(ctx context.Context, id string) error {
	last, err := s.deleteTicket(ctx, id)
	if err != nil {
		return err
	}
	if last != nil {
		s.dropTaskResources(last.ID)
	}
	if s.Events != nil && last != nil {
		if err := s.Events.Publish(ctx, domain.TicketDeleted{TicketID: last.ID, ProjectID: last.ProjectID}); err != nil {
			slog.Warn("announce task deletion", "ticket", id, "err", err)
		}
	}
	return nil
}

func (s *Service) deleteTicket(ctx context.Context, id string) (*domain.Ticket, error) {
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	// The task is deleted whatever happens to the request, so what it
	// announces is read to the end too.
	last := s.tickets.Get(context.WithoutCancel(ctx), id)
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
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "format must be markdown, html or text"}
	}
	if scope == "" {
		scope = domain.DocumentScopeProject
	}
	if !scope.Valid() {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "scope must be task or project"}
	}
	if s.projects.Get(context.TODO(), projectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	d, err := s.documents.Create(projectID, title, content, format, scope)
	return s.documentChanged(d, err, "", domain.DocumentCreated)
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
	d, err := s.documents.SetScope(id, scope)
	return s.documentChanged(d, err, "", domain.DocumentScopeChanged)
}

// UpdateDocument replaces title and content. An empty format keeps the
// stored one, so callers that never learned about formats are unaffected.
func (s *Service) UpdateDocument(id, title, content string, format domain.DocumentFormat) (*domain.Document, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	if format != "" && !format.Valid() {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "format must be markdown, html or text"}
	}
	d, err := s.documents.Update(id, title, content, format)
	return s.documentChanged(d, err, "", domain.DocumentUpdated)
}

// WriteDocumentIfVersion replaces title, content and format (empty keeps
// the stored one) while the stored content is still at version: a
// compare-and-set, so a write made on an older copy is DOCUMENT_CHANGED and
// nothing is stored. No format's shape is checked.
func (s *Service) WriteDocumentIfVersion(id, version, title, content string, format domain.DocumentFormat) (*domain.Document, error) {
	if title == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "title is required"}
	}
	if format != "" && !format.Valid() {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "format must be markdown, html or text"}
	}
	d := s.documents.Get(id)
	if d == nil {
		return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_FOUND", Message: "document not found"}
	}
	if now := domain.DocumentVersion(d.Content); now != version {
		return nil, &domain.StructuredError{Code: "DOCUMENT_CHANGED", Message: "now at " + now}
	}
	written, err := s.documents.UpdateIfContent(id, d.Content, title, content, format)
	return s.documentChanged(written, err, "", domain.DocumentUpdated)
}

func (s *Service) DeleteDocument(id string) error {
	last := s.documents.Get(id)
	if err := s.documents.Delete(id); err != nil {
		return err
	}
	_, err := s.documentChanged(last, nil, "", domain.DocumentDeleted)
	return err
}

// documentChanged raises DocumentChanged for d once it is stored (err nil),
// and answers d and err as they came.
func (s *Service) documentChanged(d *domain.Document, err error, ticketID string, change domain.DocumentChange) (*domain.Document, error) {
	if err == nil && d != nil {
		s.raise(context.Background(), domain.DocumentChanged{DocumentID: d.ID, ProjectID: d.ProjectID, TicketID: ticketID, Change: change})
	}
	return d, err
}

// --- Links ---

func (s *Service) LinkDocument(ticketID, documentID string) error {
	tk := s.tickets.Get(context.TODO(), ticketID)
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
	err := s.documents.Link(ticketID, documentID)
	_, err = s.documentChanged(doc, err, ticketID, domain.DocumentLinked)
	return err
}

func (s *Service) UnlinkDocument(ticketID, documentID string) error {
	err := s.documents.Unlink(ticketID, documentID)
	_, err = s.documentChanged(s.documents.Get(documentID), err, ticketID, domain.DocumentUnlinked)
	return err
}

func (s *Service) ListTicketDocuments(ctx context.Context, ticketID string) []*domain.Document {
	return s.documents.ListByTicket(ctx, ticketID)
}

// ListTasksDocuments is the documents linked to any of ticketIDs, each once,
// read at once.
func (s *Service) ListTasksDocuments(ctx context.Context, ticketIDs []string) []*domain.Document {
	return s.documents.ListByTickets(ctx, ticketIDs)
}

// ListDocumentTickets is the tickets a document is linked to, as stored; a
// link whose ticket is gone is skipped.
func (s *Service) ListDocumentTickets(documentID string) []*domain.Ticket {
	var out []*domain.Ticket
	for _, id := range s.documents.ListTicketIDsByDocument(documentID) {
		if tk := s.tickets.Get(context.TODO(), id); tk != nil {
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
