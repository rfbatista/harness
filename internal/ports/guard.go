package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// GuardSessions keeps each session's guard configuration (ADR 0011, 0012): the
// value taken from the agent system settings when the session started or
// resumed, which the Guard layer reads for the whole run.
type GuardSessions interface {
	// Keep records cfg as the session's guard configuration, replacing the
	// one a start or an earlier resume kept.
	Keep(ctx context.Context, sessionID string, cfg domain.GuardConfig) error
	// Config answers the session's kept configuration; ok is false for a
	// session that kept none (it is not gated).
	Config(ctx context.Context, sessionID string) (cfg domain.GuardConfig, ok bool, err error)
}
