package ports

import (
	"context"
	"io"
	"strings"
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
	// Output writes one gate's output to w as it is written while its run is
	// going, and returns when the gate has ended; for a finished run it writes
	// the recorded output. An unknown run or gate answers GATE_RUN_NOT_FOUND.
	Output(ctx context.Context, sessionID, runID string, kind domain.GateKind, w io.Writer) error
}

// GateRunChange is a gate run starting or finishing, as the project feed
// carries it; finished adds the result and whether the run counted.
type GateRunChange struct {
	ProjectID string             `json:"project_id"`
	SessionID string             `json:"session_id"`
	RunID     string             `json:"run_id"`
	Trigger   domain.GateTrigger `json:"trigger"`
	// Phase is "started" or "finished".
	Phase   string               `json:"phase"`
	Result  domain.GateRunResult `json:"result,omitempty"`
	Counted bool                 `json:"counted,omitempty"`
}

// SessionEscalatedChange is a gated session stopped for reaching its retry
// limit, as the project feed carries it.
type SessionEscalatedChange struct {
	ProjectID string            `json:"project_id"`
	SessionID string            `json:"session_id"`
	RunID     string            `json:"run_id"`
	RedGates  []domain.GateKind `json:"red_gates"`
}

// DeclarationRefusal is one check refusing a done declaration.
type DeclarationRefusal struct {
	Code    string
	Message string
	// Counts says the refusal counts toward the session's retry limit.
	Counts bool
}

// Declaration is the answer to a done declaration: accepted when every check
// passed, else refused with every refusing check's reason. Run is the gate run
// the declaration made, nil when none ran.
type Declaration struct {
	Accepted bool
	Run      *domain.GateRun
	Refusals []DeclarationRefusal
}

// Declarations is the done declaration and the practice run as the session
// tools use them.
type Declarations interface {
	// Declare makes the session's done declaration: the declaration checks run,
	// the gate run first.
	Declare(ctx context.Context, sessionID string) (Declaration, error)
	// Practice runs the gates (only kinds, when given) as the agent's own
	// practice run: recorded, never counted.
	Practice(ctx context.Context, sessionID string, kinds []domain.GateKind) (*domain.GateRun, error)
}

// Message is every refusal's reason, one after the other.
func (d Declaration) Message() string {
	parts := make([]string, len(d.Refusals))
	for i, r := range d.Refusals {
		parts[i] = r.Message
	}
	return strings.Join(parts, "\n\n")
}
