package ports

import (
	"context"
	"io"
	"time"

	"operators-mcp/internal/domain"
)

// GateRunStore keeps every gate run of every session (ADR 0012).
type GateRunStore interface {
	// Save records the run, replacing one with the same id.
	Save(ctx context.Context, run *domain.GateRun) error
	// List answers the session's gate runs, oldest first.
	List(ctx context.Context, sessionID string) ([]domain.GateRun, error)
}

// GateExit is how one gate command ended.
type GateExit struct {
	ExitCode int
	TimedOut bool
	Duration time.Duration
}

// GateExecutor runs one gate command in a session's worktree under its sandbox
// profile, writing the output to out, and answers how it ended. A command that
// cannot start answers exit code -1 and the reason in out.
type GateExecutor interface {
	Execute(ctx context.Context, sess *domain.Session, command string, timeout time.Duration, out io.Writer) GateExit
}

// GateRunRequest asks for a gate run of a session.
type GateRunRequest struct {
	SessionID string
	Trigger   domain.GateTrigger
	// Kinds limits the run to these gate kinds; empty runs every gate.
	Kinds []domain.GateKind
}

// GateRuns is the gate runner as the HTTP API and the session tools use it.
type GateRuns interface {
	// Run runs the session's gates in its worktree and records the run. A
	// session that is not gated answers SESSION_NOT_GATED.
	Run(ctx context.Context, req GateRunRequest) (*domain.GateRun, error)
	// List answers the session's recorded gate runs, oldest first.
	List(ctx context.Context, sessionID string) ([]domain.GateRun, error)
}
