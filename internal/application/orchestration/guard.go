package orchestration

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The Guard layer's configuration of a session is taken from the agent system
// settings once when the session starts or resumes and kept with it (ADR 0011,
// 0012), as the memory budget is (ADR 0008): the gate runner and the
// enforcement read it from GuardSessions, never from the settings again.

// resolveGuard resolves the effective guard configuration for a session about
// to start or resume in mode, with the scope it keeps. When the settings store
// cannot answer, the harness defaults stand in, with the reason on the
// configuration: the gated rule depends only on the mode and the diff, so a
// store fault never switches the Guard layer off. It reads the settings once;
// the start or resume passes the result to the sandbox policy and keeps it.
func (s *Service) resolveGuard(ctx context.Context, projectID, repositoryID string, mode domain.SessionMode, scope []string, testsFirst bool) domain.GuardConfig {
	ctx = context.WithoutCancel(ctx)
	var (
		eff domain.EffectiveAgentSystem
		err error
	)
	if s.AgentSystem == nil {
		err = errors.New("no settings store")
	} else {
		eff, err = s.AgentSystem.Effective(ctx, projectID, repositoryID)
	}
	var reason string
	if err != nil {
		slog.Warn("guard configuration not resolved; the harness defaults stand in", "project", projectID, "err", err)
		reason = "settings unavailable: " + err.Error()
		eff = domain.ResolveAgentSystem(domain.AgentSystemHarnessDefaults{})
	}
	cfg := domain.NewGuardConfig(eff, mode)
	cfg.Unresolved = reason
	cfg.Scope = slices.Clone(scope)
	cfg.TestsFailFirst = testsFirst
	return cfg
}

// keptChoices are what a session was given at its start and a resume carries
// on: the architect's scope and tests-fail-first.
func (s *Service) keptChoices(ctx context.Context, sessionID string) (scope []string, testsFirst bool) {
	if s.GuardSessions == nil {
		return nil, false
	}
	if prev, ok, err := s.GuardSessions.Config(ctx, sessionID); err == nil && ok {
		return prev.Scope, prev.TestsFailFirst
	}
	return nil, false
}

// keepGuard keeps cfg as the session's guard configuration for its whole run.
func (s *Service) keepGuard(ctx context.Context, sess *domain.Session, cfg domain.GuardConfig) {
	if s.GuardSessions == nil || sess == nil || sess.ProjectID == "" {
		return
	}
	if err := s.GuardSessions.Keep(context.WithoutCancel(ctx), sess.ID, cfg); err != nil {
		slog.Warn("guard configuration not kept", "session", sess.ID, "err", err)
	}
}

// resumeGuardCount tells the guard store of a resume: a session the harness
// stopped at its retry limit is given another chance, with its red count started
// over; any other keeps the count it had.
func (s *Service) resumeGuardCount(ctx context.Context, sessionID string) {
	if s.GuardSessions == nil {
		return
	}
	if err := s.GuardSessions.ResumeRed(context.WithoutCancel(ctx), sessionID); err != nil {
		slog.Warn("guard count not updated on resume", "session", sessionID, "err", err)
	}
}

// GuardConfig is the guard configuration the session kept at its start or last
// resume; ok is false when it kept none.
func (s *Service) GuardConfig(ctx context.Context, sessionID string) (domain.GuardConfig, bool) {
	if s.GuardSessions == nil {
		return domain.GuardConfig{}, false
	}
	cfg, ok, err := s.GuardSessions.Config(ctx, sessionID)
	if err != nil {
		slog.Warn("guard configuration not read", "session", sessionID, "err", err)
		return domain.GuardConfig{}, false
	}
	return cfg, ok
}

// GateWrap is the sandbox profile a gate run of the session executes under:
// the one the session's own commands run in. It answers a nil wrap when the
// session is not sandboxed.
func (s *Service) GateWrap(ctx context.Context, sess *domain.Session) (func(ports.SandboxCommand) (ports.SandboxCommand, error), error) {
	var guard *domain.GuardConfig
	if cfg, ok := s.GuardConfig(ctx, sess.ID); ok {
		guard = &cfg
	}
	sb, err := s.resumeSandbox(ctx, sess, nil, nil, guard)
	if err != nil || sb.policy == nil || s.Sandbox == nil || s.Sandbox.Driver == nil {
		return nil, err
	}
	policy, driver := *sb.policy, s.Sandbox.Driver
	return func(c ports.SandboxCommand) (ports.SandboxCommand, error) { return driver.Wrap(policy, c) }, nil
}
