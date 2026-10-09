package planning

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Comments are written by the person (AddTicketComment and the rest, over
// the API) or by a session on its own task (AddSessionComment and the rest,
// from its MCP tools). The person deletes any comment but edits only their
// own; a session edits and deletes only what it wrote.

func (s *Service) ListTicketComments(_ context.Context, ticketID string, since time.Time) ([]*domain.TicketComment, error) {
	if s.Comments == nil {
		return nil, errNoComments
	}
	if _, err := s.ticket(ticketID); err != nil {
		return nil, err
	}
	return s.Comments.List(ticketID, since), nil
}

func (s *Service) AddTicketComment(ctx context.Context, ticketID, body string) (*domain.TicketComment, error) {
	return s.addComment(ctx, ticketID, domain.TicketComment{Author: domain.CommentByPerson}, body)
}

// AddSessionComment writes as session w. It raises the same event as the
// person's comment; orchestration decides that it wakes no one.
func (s *Service) AddSessionComment(ctx context.Context, ticketID string, w domain.CommentWriter, body string) (*domain.TicketComment, error) {
	if s.Comments == nil {
		return nil, errNoComments
	}
	if w.SessionID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "a session is required"}
	}
	return s.addComment(ctx, ticketID, domain.TicketComment{Author: domain.CommentBySession, SessionID: w.SessionID, AgentName: w.AgentName}, body)
}

// addComment stores body on the task under author's fields.
func (s *Service) addComment(ctx context.Context, ticketID string, author domain.TicketComment, body string) (*domain.TicketComment, error) {
	if s.Comments == nil {
		return nil, errNoComments
	}
	tk, err := s.ticket(ticketID)
	if err != nil {
		return nil, err
	}
	b, err := domain.CommentBody(body)
	if err != nil {
		return nil, err
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	if err := s.stillThere(tk.ID); err != nil {
		return nil, err
	}
	author.TicketID, author.ProjectID, author.Body = tk.ID, tk.ProjectID, b
	c, err := s.Comments.Create(&author)
	if err != nil {
		return nil, err
	}
	s.announceChange(ports.TicketCommentUpdated(c))
	s.raise(ctx, domain.TicketCommentAdded{Comment: *c})
	return c, nil
}

// UpdateTicketComment replaces the body of the person's comment; the same
// body again changes nothing, updated_at included.
func (s *Service) UpdateTicketComment(_ context.Context, id, body string) (*domain.TicketComment, error) {
	if s.Comments == nil {
		return nil, errNoComments
	}
	b, err := domain.CommentBody(body)
	if err != nil {
		return nil, err
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	existing := s.Comments.Get(id)
	if existing == nil {
		return nil, errCommentNotFound
	}
	if existing.Author == domain.CommentBySession {
		return nil, errCommentNotYours
	}
	return s.setCommentBody(existing, b)
}

// UpdateSessionComment replaces the body of a comment sessionID wrote on
// ticketID; the same body again changes nothing.
func (s *Service) UpdateSessionComment(_ context.Context, ticketID, sessionID, id, body string) (*domain.TicketComment, error) {
	if s.Comments == nil {
		return nil, errNoComments
	}
	if id == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "comment_id is required"}
	}
	b, err := domain.CommentBody(body)
	if err != nil {
		return nil, err
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	existing, err := s.sessionsComment(ticketID, sessionID, id)
	if err != nil {
		return nil, err
	}
	return s.setCommentBody(existing, b)
}

// setCommentBody writes b over existing and announces it, unless it is the
// same body. The caller holds resourceMu.
func (s *Service) setCommentBody(existing *domain.TicketComment, b string) (*domain.TicketComment, error) {
	if existing.Body == b {
		return existing, nil
	}
	c, err := s.Comments.SetBody(existing.ID, b)
	if err != nil {
		return nil, err
	}
	s.announceChange(ports.TicketCommentUpdated(c))
	return c, nil
}

// DeleteTicketComment removes any comment of the task: the person moderates
// what sessions write there too.
func (s *Service) DeleteTicketComment(_ context.Context, id string) error {
	if s.Comments == nil {
		return errNoComments
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	existing := s.Comments.Get(id)
	if existing == nil {
		return errCommentNotFound
	}
	return s.removeComment(existing)
}

// DeleteSessionComment removes a comment sessionID wrote on ticketID.
func (s *Service) DeleteSessionComment(_ context.Context, ticketID, sessionID, id string) error {
	if s.Comments == nil {
		return errNoComments
	}
	if id == "" {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "comment_id is required"}
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	existing, err := s.sessionsComment(ticketID, sessionID, id)
	if err != nil {
		return err
	}
	return s.removeComment(existing)
}

// removeComment deletes existing and announces it. The caller holds
// resourceMu.
func (s *Service) removeComment(existing *domain.TicketComment) error {
	if err := s.Comments.Delete(existing.ID); err != nil {
		return err
	}
	s.announceChange(ports.TicketCommentRemoved(existing))
	return nil
}

// sessionsComment is comment id when sessionID wrote it on ticketID. The
// task is checked before the author, so another task's comment never tells
// who wrote it. The caller holds resourceMu.
func (s *Service) sessionsComment(ticketID, sessionID, id string) (*domain.TicketComment, error) {
	c := s.Comments.Get(id)
	switch {
	case c == nil:
		return nil, errCommentNotFound
	case c.TicketID != ticketID:
		return nil, errCommentNotOnTask
	case !c.WrittenBy(sessionID):
		return nil, errCommentNotYours
	}
	return c, nil
}
