package gateruns

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"

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
	runs    *Service
	checks  []DeclarationCheck
	mu      sync.Mutex
	flights map[string]*flight
	// Escalation takes over when a red declaration reaches the retry limit; nil:
	// the count is kept and nothing else happens.
	Escalation Escalation
}

// Escalation is what the harness does when a session's red declarations reach
// its retry limit: stop it, tell the person (and the architect, for a
// delegate), and publish the event. run is the declaration that reached it.
type Escalation interface {
	Escalate(ctx context.Context, sess *domain.Session, run *domain.GateRun) error
}

// NewDeclarer returns the declarer, with the gate run as its first check.
func NewDeclarer(runs *Service) *Declarer { return &Declarer{runs: runs} }

// Add registers another declaration check, run after the gate run.
func (d *Declarer) Add(c DeclarationCheck) { d.checks = append(d.checks, c) }

// flight is a declaration in progress, which a second one for the same session
// joins instead of starting its own.
type flight struct {
	done chan struct{}
	res  ports.Declaration
	err  error
}

// Declare implements ports.Declarations. Declarations of one session are
// single-flight: one made while another runs waits for it and gets its answer,
// so two at once give one gate run, one count and at most one escalation.
func (d *Declarer) Declare(ctx context.Context, sessionID string) (ports.Declaration, error) {
	d.mu.Lock()
	if f, ok := d.flights[sessionID]; ok {
		d.mu.Unlock()
		select {
		case <-f.done:
			return f.res, f.err
		case <-ctx.Done():
			return ports.Declaration{}, ctx.Err()
		}
	}
	f := &flight{done: make(chan struct{})}
	if d.flights == nil {
		d.flights = map[string]*flight{}
	}
	d.flights[sessionID] = f
	d.mu.Unlock()

	f.res, f.err = d.declare(ctx, sessionID)

	d.mu.Lock()
	delete(d.flights, sessionID)
	d.mu.Unlock()
	close(f.done)
	return f.res, f.err
}

func (d *Declarer) declare(ctx context.Context, sessionID string) (ports.Declaration, error) {
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
	if err := d.count(ctx, sess, &out); err != nil {
		return ports.Declaration{}, err
	}
	return out, nil
}

// count keeps the session's consecutive red declarations: a refusal that counts
// adds one and a limit reached escalates; an accepted declaration starts the
// count again; any other refusal leaves it as it is.
func (d *Declarer) count(ctx context.Context, sess *domain.Session, out *ports.Declaration) error {
	if out.Run == nil {
		return nil
	}
	if out.Accepted {
		return d.runs.d.Guard.ResetRed(ctx, sess.ID)
	}
	counted := slices.ContainsFunc(out.Refusals, func(r ports.DeclarationRefusal) bool { return r.Counts })
	if !counted {
		return nil
	}
	n, err := d.runs.d.Guard.CountRed(ctx, sess.ID)
	if err != nil {
		return err
	}
	_, cfg, err := d.runs.Gated(ctx, sess.ID)
	if err != nil {
		return err
	}
	limit := max(cfg.RetryLimit, domain.MinRetryLimit)
	if n < limit {
		note := "\n\nRed declaration " + strconv.Itoa(n) + " of " + strconv.Itoa(limit) + ": at " + strconv.Itoa(limit) + " the harness stops this session and tells the person."
		out.Refusals[0].Message += note
		return nil
	}
	out.Run.Escalated = true
	if err := d.runs.d.Store.Save(ctx, out.Run); err != nil {
		return err
	}
	out.Refusals[0].Message += "\n\nRed declaration " + strconv.Itoa(n) + " of " + strconv.Itoa(limit) + ": this session is stopped as escalated and the person is told."
	if d.Escalation == nil {
		return nil
	}
	return d.Escalation.Escalate(ctx, sess, out.Run)
}

// Practice implements ports.Declarations.
func (d *Declarer) Practice(ctx context.Context, sessionID string, kinds []domain.GateKind) (*domain.GateRun, error) {
	return d.runs.Run(ctx, ports.GateRunRequest{SessionID: sessionID, Trigger: domain.GateTriggerPractice, Kinds: kinds})
}

func isNotGated(err error) bool {
	var se *domain.StructuredError
	return errors.As(err, &se) && se.Code == "SESSION_NOT_GATED"
}

// HasDiff implements ports.Declarations.
func (d *Declarer) HasDiff(ctx context.Context, sessionID string) bool {
	if d.runs.d.HasDiff == nil {
		return false
	}
	sess, err := d.runs.d.Sessions.Find(ctx, sessionID)
	if err != nil || sess == nil {
		return false
	}
	return d.runs.d.HasDiff(ctx, sess)
}
