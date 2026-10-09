package planning

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

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
	c, err := s.Comments.Create(&domain.TicketComment{TicketID: tk.ID, ProjectID: tk.ProjectID, Author: domain.CommentByPerson, Body: b})
	if err != nil {
		return nil, err
	}
	s.announceChange(ports.TicketCommentUpdated(c))
	s.raise(ctx, domain.TicketCommentAdded{Comment: *c})
	return c, nil
}

// UpdateTicketComment replaces the body; the same body again changes
// nothing, updated_at included.
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
	if existing.Body == b {
		return existing, nil
	}
	c, err := s.Comments.SetBody(id, b)
	if err != nil {
		return nil, err
	}
	s.announceChange(ports.TicketCommentUpdated(c))
	return c, nil
}

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
	if err := s.Comments.Delete(id); err != nil {
		return err
	}
	s.announceChange(ports.TicketCommentRemoved(existing))
	return nil
}
