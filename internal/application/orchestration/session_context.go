package orchestration

import (
	"context"

	"operators-mcp/internal/domain"
)

// SessionContext reads what session id's context was when claude started and
// resumed it, its use of tools and skills, and what changed (ADR 0099). A
// session that started before the harness recorded context reads as not
// recorded.
func (s *Service) SessionContext(ctx context.Context, id string) (domain.SessionContextView, error) {
	if s.sessions.Get(id) == nil {
		return domain.SessionContextView{}, sessionContextNotFound()
	}
	c, err := s.loadContext(ctx, id)
	if err != nil {
		return domain.SessionContextView{}, err
	}
	return c.View(id), nil
}

// SessionContextPiece reads the whole text of one piece of one entry.
func (s *Service) SessionContextPiece(ctx context.Context, id, entryID, pieceID string) (string, error) {
	if s.sessions.Get(id) == nil {
		return "", sessionContextNotFound()
	}
	c, err := s.loadContext(ctx, id)
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", &domain.StructuredError{Code: "CONTEXT_ENTRY_NOT_FOUND", Message: "context entry not found"}
	}
	entry, ok := c.Entry(entryID)
	if !ok {
		return "", &domain.StructuredError{Code: "CONTEXT_ENTRY_NOT_FOUND", Message: "context entry not found"}
	}
	piece, ok := entry.Piece(pieceID)
	if !ok {
		return "", &domain.StructuredError{Code: "CONTEXT_PIECE_NOT_FOUND", Message: "context piece not found"}
	}
	return piece.Text, nil
}

func sessionContextNotFound() error {
	return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
}

// loadContext reads the stored context: nil when there is none, or when this
// server stores none.
func (s *Service) loadContext(ctx context.Context, id string) (*domain.SessionContext, error) {
	if s.Contexts == nil {
		return nil, nil
	}
	return s.Contexts.Load(ctx, id)
}

// recordEntry stores e as the session's newest context entry and keeps the
// earlier ones.
func (s *Service) recordEntry(ctx context.Context, id string, e domain.ContextEntry) error {
	if s.Contexts == nil {
		return nil
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	c, err := s.Contexts.Load(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		c = &domain.SessionContext{SessionID: id}
	}
	c.Entries = append(c.Entries, e)
	return s.Contexts.Save(ctx, c)
}
