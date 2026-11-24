package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// SessionContextStore keeps what each session's context was when claude
// started or resumed it (ADR 0099). It is kept for the session's life.
type SessionContextStore interface {
	// Load returns the session's stored context: nil, nil when it has none (a
	// session that started before the harness recorded context).
	Load(ctx context.Context, sessionID string) (*domain.SessionContext, error)
	// Save replaces the session's stored context.
	Save(ctx context.Context, c *domain.SessionContext) error
	// Delete removes the session's stored context; none is not an error.
	Delete(ctx context.Context, sessionID string) error
}
