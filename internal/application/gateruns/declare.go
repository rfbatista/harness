package gateruns

import (
	"context"
	"errors"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.Declarations = (*Declarer)(nil)

// CheckResult is one declaration check's answer: pass, or refuse with a
// message. Counts says the refusal counts toward the retry limit.
type CheckResult struct {
	Refused bool
	Code    string
	Message string
	Counts  bool
}

// DeclarationCheck is one thing a done declaration must satisfy. The gate run
// is the first; another layer adds its own beside it with Declarer.Add (the
// Learn layer's task-memory check).
type DeclarationCheck interface {
	Check(ctx context.Context, sess *domain.Session) (CheckResult, error)
}

// Declarer makes done declarations: every check runs, and the declaration
// passes only when none refuses, so a red gate run and another layer's
// refusal are reported together.
type Declarer struct {
	runs   *Service
	checks []DeclarationCheck
}

// NewDeclarer returns the declarer, with the gate run as its first check.
func NewDeclarer(runs *Service) *Declarer { return &Declarer{runs: runs} }

// Add registers another declaration check, run after the gate run.
func (d *Declarer) Add(c DeclarationCheck) { d.checks = append(d.checks, c) }

// Declare implements ports.Declarations.
func (d *Declarer) Declare(ctx context.Context, sessionID string) (ports.Declaration, error) {
	sess, err := d.runs.d.Sessions.Find(ctx, sessionID)
	if err != nil {
		return ports.Declaration{}, err
	}
	if sess == nil {
		return ports.Declaration{}, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	var out ports.Declaration
	run, err := d.runs.Run(ctx, ports.GateRunRequest{SessionID: sessionID, Trigger: domain.GateTriggerDeclaration})
	switch {
	case err == nil:
		out.Run = run
		switch run.Result {
		case domain.GateRunRed:
			out.Refusals = append(out.Refusals, ports.DeclarationRefusal{Code: "GATES_RED", Message: run.FailureReport(), Counts: true})
		case domain.GateRunNotPassed:
			out.Refusals = append(out.Refusals, ports.DeclarationRefusal{Code: "REQUIRED_GATE_MISSING", Message: run.FailureReport()})
		}
	case isNotGated(err):
		// A session that is not gated has no gates to pass.
	default:
		return ports.Declaration{}, err
	}
	for _, c := range d.checks {
		res, err := c.Check(ctx, sess)
		if err != nil {
			return ports.Declaration{}, err
		}
		if res.Refused {
			out.Refusals = append(out.Refusals, ports.DeclarationRefusal{Code: res.Code, Message: res.Message, Counts: res.Counts})
		}
	}
	out.Accepted = len(out.Refusals) == 0
	return out, nil
}

// Practice implements ports.Declarations.
func (d *Declarer) Practice(ctx context.Context, sessionID string, kinds []domain.GateKind) (*domain.GateRun, error) {
	return d.runs.Run(ctx, ports.GateRunRequest{SessionID: sessionID, Trigger: domain.GateTriggerPractice, Kinds: kinds})
}

func isNotGated(err error) bool {
	var se *domain.StructuredError
	return errors.As(err, &se) && se.Code == "SESSION_NOT_GATED"
}
