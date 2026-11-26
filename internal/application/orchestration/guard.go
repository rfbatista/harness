package orchestration

import (
	"context"
	"errors"
	"log/slog"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The Guard layer's configuration of a session is taken from the agent system
// settings once when the session starts or resumes and kept with it (ADR 0011,
// 0012), as the memory budget is (ADR 0008): the gate runner and the
// enforcement read it from GuardSessions, never from the settings again.

// keepGuard resolves the effective guard configuration for the session's
// repository and keeps it. When the settings store cannot answer, the harness
// defaults stand in, with the reason on the configuration: the gated rule
// depends only on the mode and the diff, so a store fault never switches the
// Guard layer off.
func (s *Service) keepGuard(ctx context.Context, sess *domain.Session) {
	if s.GuardSessions == nil || sess == nil || sess.ProjectID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	var (
		eff domain.EffectiveAgentSystem
		err error
	)
	if s.AgentSystem == nil {
		err = errors.New("no settings store")
	} else {
		eff, err = s.AgentSystem.Effective(ctx, sess.ProjectID, sess.RepositoryID)
	}
	var reason string
	if err != nil {
		slog.Warn("guard configuration not resolved; the harness defaults stand in", "session", sess.ID, "err", err)
		reason = "settings unavailable: " + err.Error()
		eff = domain.ResolveAgentSystem(domain.AgentSystemHarnessDefaults{})
	}
	cfg := domain.NewGuardConfig(eff, sess.Mode)
	cfg.Unresolved = reason
	if err := s.GuardSessions.Keep(ctx, sess.ID, cfg); err != nil {
		slog.Warn("guard configuration not kept", "session", sess.ID, "err", err)
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
	sb, err := s.resumeSandbox(ctx, sess, nil, nil)
	if err != nil || sb.policy == nil || s.Sandbox == nil || s.Sandbox.Driver == nil {
		return nil, err
	}
	policy, driver := *sb.policy, s.Sandbox.Driver
	return func(c ports.SandboxCommand) (ports.SandboxCommand, error) { return driver.Wrap(policy, c) }, nil
}
