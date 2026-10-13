// Package planning provides ticket and document use-cases: CRUD plus the
// many-to-many link between tickets and documents. It is deliberately kept
// separate from the catalog contexts so the planning domain stays isolated.
package planning

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rfbatista/harnesskit/errs"

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
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "format must be markdown, html or text"}
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

// EditDocument makes an exact string-replace edit to an HTML document's
// stored content and writes it with a compare-and-set on the content it
// read. With no expected version, a write that loses a race is tried once
// more on the fresh content; with one, the caller decides.
func (s *Service) EditDocument(id, old, new string, replaceAll bool, expectedVersion string) (*domain.Document, error) {
	if old == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "old_string is required"}
	}
	if old == new {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "old_string and new_string are the same; there is nothing to change"}
	}
	for attempt := 0; ; attempt++ {
		d, err := s.editOnce(id, old, new, replaceAll, expectedVersion)
		if !errs.HasCode(err, "DOCUMENT_CHANGED") || expectedVersion != "" || attempt == 1 {
			return d, err
		}
	}
}

func (s *Service) editOnce(id, old, new string, replaceAll bool, expectedVersion string) (*domain.Document, error) {
	d := s.documents.Get(id)
	if d == nil {
		return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_FOUND", Message: "document not found"}
	}
	if d.Format != domain.DocumentFormatHTML {
		return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_HTML",
			Message: "this document is Markdown; rewrite it once as HTML with update_*_document, then edit"}
	}
	if version := domain.DocumentVersion(d.Content); expectedVersion != "" && expectedVersion != version {
		return nil, &domain.StructuredError{Code: "DOCUMENT_CHANGED", Message: "now at " + version + "; read it again before editing"}
	}
	switch n := strings.Count(d.Content, old); {
	case n == 0:
		return nil, &domain.StructuredError{Code: "EDIT_NO_MATCH", Message: noMatchMessage(d.Content, old)}
	case n > 1 && !replaceAll:
		return nil, &domain.StructuredError{Code: "EDIT_AMBIGUOUS", Message: strconv.Itoa(n) +
			" matches; add surrounding text to old_string so it matches once, or set replace_all"}
	}
	limit := 1
	if replaceAll {
		limit = -1
	}
	content := strings.Replace(d.Content, old, new, limit)
	if !domain.IsHTMLDocument(content) {
		return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_HTML",
			Message: "the edit would leave the document without a complete HTML page (doctype, html, body); nothing was stored"}
	}
	edited, err := s.documents.UpdateIfContent(id, d.Content, d.Title, content, "")
	if errs.HasCode(err, "DOCUMENT_CHANGED") {
		return nil, &domain.StructuredError{Code: "DOCUMENT_CHANGED", Message: "the document changed while it was being edited; read it again"}
	}
	return s.documentChanged(edited, err, "", domain.DocumentUpdated)
}

// noMatchMessage says old_string was not found, with the stored lines that
// come closest, so the agent can copy the markup exactly.
func noMatchMessage(content, old string) string {
	msg := "old_string is not in the document; it must match the stored markup exactly (read it with view raw)"
	first := strings.SplitN(strings.TrimSpace(old), "\n", 2)[0]
	if first == "" {
		return msg
	}
	best, bestScore := "", 0
	for _, line := range strings.Split(content, "\n") {
		if score := commonPrefixRunes(strings.TrimSpace(line), strings.TrimSpace(first)); score > bestScore {
			best, bestScore = strings.TrimSpace(line), score
		}
	}
	if bestScore >= 8 {
		if len(best) > 300 {
			best = best[:300] + "…"
		}
		msg += "; closest stored line: " + strconv.Quote(best)
	}
	return msg
}

// commonPrefixRunes is the length of the longest common prefix of a and b.
func commonPrefixRunes(a, b string) int {
	n := 0
	for i := 0; i < len(a) && i < len(b) && a[i] == b[i]; i++ {
		n++
	}
	return n
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
	err := s.documents.Link(ticketID, documentID)
	_, err = s.documentChanged(doc, err, ticketID, domain.DocumentLinked)
	return err
}

func (s *Service) UnlinkDocument(ticketID, documentID string) error {
	err := s.documents.Unlink(ticketID, documentID)
	_, err = s.documentChanged(s.documents.Get(documentID), err, ticketID, domain.DocumentUnlinked)
	return err
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
