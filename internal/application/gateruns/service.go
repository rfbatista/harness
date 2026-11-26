// Package gateruns is the Guard layer's gate runner: it runs a gated session's
// gates in its worktree, without the agent, and records the run (ADR 0012,
// 0014).
package gateruns

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"slices"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GateRuns = (*Service)(nil)

// Deps are what the runner reads and drives.
type Deps struct {
	Sessions interface {
		Find(ctx context.Context, id string) (*domain.Session, error)
	}
	// Guard gives the configuration the session kept at its start.
	Guard ports.GuardSessions
	Store ports.GateRunStore
	Exec  ports.GateExecutor
	// HasDiff says whether a mode-less session has a diff against its base;
	// nil: it has none.
	HasDiff func(ctx context.Context, sess *domain.Session) bool
	// Announce puts a gate run's start and end on the project feed; nil: none.
	Announce ports.ProjectChangeSink
	Now      func() time.Time
}

// Service is the gate runner.
type Service struct {
	d    Deps
	live liveRuns
}

// New returns the runner.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d}
}

func notGated(reason string) error {
	return &domain.StructuredError{Code: "SESSION_NOT_GATED", Message: "the session is not gated: " + reason}
}

// Gated answers the session and the guard configuration it kept when it is
// gated, SESSION_NOT_FOUND or SESSION_NOT_GATED otherwise.
func (s *Service) Gated(ctx context.Context, sessionID string) (*domain.Session, domain.GuardConfig, error) {
	sess, err := s.d.Sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, domain.GuardConfig{}, err
	}
	if sess == nil {
		return nil, domain.GuardConfig{}, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	cfg, ok, err := s.d.Guard.Config(ctx, sessionID)
	if err != nil {
		return nil, domain.GuardConfig{}, err
	}
	if !ok {
		return nil, domain.GuardConfig{}, notGated("it kept no guard configuration")
	}
	hasDiff := false
	if cfg.Mode == domain.SessionModeDefault && s.d.HasDiff != nil {
		hasDiff = s.d.HasDiff(ctx, sess)
	}
	if !cfg.Gated(hasDiff) {
		return nil, domain.GuardConfig{}, notGated("its mode is not gated")
	}
	return sess, cfg, nil
}

// Run implements ports.GateRuns.
func (s *Service) Run(ctx context.Context, req ports.GateRunRequest) (*domain.GateRun, error) {
	sess, cfg, err := s.Gated(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	run := &domain.GateRun{ID: newID(), SessionID: sess.ID, Trigger: req.Trigger, Kinds: slices.Clone(req.Kinds), StartedAt: s.d.Now()}
	wanted := func(k domain.GateKind) bool { return len(req.Kinds) == 0 || slices.Contains(req.Kinds, k) }
	for _, k := range cfg.MissingRequiredGates {
		if wanted(k) {
			run.MissingKinds = append(run.MissingKinds, k)
		}
	}
	var planned []domain.GuardGate
	if len(run.MissingKinds) == 0 {
		for _, g := range cfg.Gates {
			if g.Command != "" && wanted(g.Kind) {
				planned = append(planned, g)
			}
		}
	}
	kinds := make([]domain.GateKind, len(planned))
	for i, g := range planned {
		kinds[i] = g.Kind
	}
	live := s.live.start(run.ID, kinds)
	defer s.live.end(run.ID)
	s.announce(sess, run, "started")
	for _, g := range planned {
		run.Gates = append(run.Gates, s.runGate(ctx, sess, g, live[g.Kind]))
	}
	run.Finish(s.d.Now())
	if err := s.d.Store.Save(ctx, run); err != nil {
		return nil, err
	}
	s.announce(sess, run, "finished")
	return run, nil
}

func (s *Service) announce(sess *domain.Session, run *domain.GateRun, phase string) {
	if s.d.Announce == nil {
		return
	}
	change := &ports.GateRunChange{ProjectID: sess.ProjectID, SessionID: sess.ID, RunID: run.ID, Trigger: run.Trigger, Phase: phase}
	if phase == "finished" {
		change.Result, change.Counted = run.Result, run.Counted
	}
	s.d.Announce.AnnounceChange(ports.ProjectChange{GateRun: change})
}

func (s *Service) runGate(ctx context.Context, sess *domain.Session, g domain.GuardGate, live *liveGate) domain.GateResult {
	timeout := time.Duration(g.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = domain.DefaultGateTimeoutSeconds * time.Second
	}
	out := &tailBuffer{max: domain.MaxGateOutputBytes}
	exit := s.d.Exec.Execute(ctx, sess, g.Command, timeout, io.MultiWriter(out, live))
	live.finish()
	return domain.GateResult{
		Kind: g.Kind, Command: g.Command, TimeoutSeconds: int(timeout / time.Second),
		ExitCode: exit.ExitCode, TimedOut: exit.TimedOut, DurationMs: exit.Duration.Milliseconds(), Output: out.String(),
	}
}

// List implements ports.GateRuns.
func (s *Service) List(ctx context.Context, sessionID string) ([]domain.GateRun, error) {
	sess, err := s.d.Sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return s.d.Store.List(ctx, sessionID)
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }

// Output implements ports.GateRuns: the gate's output as it is written while
// its run is going, then the rest; a finished run's gate answers what was
// recorded.
func (s *Service) Output(ctx context.Context, sessionID, runID string, kind domain.GateKind, w io.Writer) error {
	if g, ok := s.live.find(runID, kind); ok {
		return g.stream(ctx, w)
	}
	runs, err := s.List(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.ID != runID {
			continue
		}
		for _, g := range r.Gates {
			if g.Kind == kind {
				_, err := io.WriteString(w, g.Output)
				return err
			}
		}
	}
	return &domain.StructuredError{Code: "GATE_RUN_NOT_FOUND", Message: "no gate " + string(kind) + " in run " + runID}
}
