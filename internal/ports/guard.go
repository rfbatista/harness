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
	// one a start or an earlier resume kept. The red count is not touched.
	Keep(ctx context.Context, sessionID string, cfg domain.GuardConfig) error
	// Config answers the session's kept configuration; ok is false for a
	// session that kept none (it is not gated).
	Config(ctx context.Context, sessionID string) (cfg domain.GuardConfig, ok bool, err error)
	// CountRed adds one consecutive red declaration run to the session's count
	// and answers the new count.
	CountRed(ctx context.Context, sessionID string) (int, error)
	// MarkEscalated records that the session was stopped at its retry limit.
	MarkEscalated(ctx context.Context, sessionID string) error
	// ResumeRed is told of each resume: a session that was escalated is given
	// another chance, so its count starts over once; any other keeps its count,
	// which only a green declaration resets.
	ResumeRed(ctx context.Context, sessionID string) error
	// ResetRed starts the session's count again, after a green declaration.
	ResetRed(ctx context.Context, sessionID string) error
}
