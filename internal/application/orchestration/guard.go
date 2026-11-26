package orchestration

import (
	"context"
	"log/slog"

	"operators-mcp/internal/domain"
)

// The Guard layer's configuration of a session is taken from the agent system
// settings once when the session starts or resumes and kept with it (ADR 0011,
// 0012), as the memory budget is (ADR 0008): the gate runner and the
// enforcement read it from GuardSessions, never from the settings again.

// keepGuard resolves the effective guard configuration for the session's
// repository and keeps it. A session the settings store cannot resolve starts
// as before, kept as not gated with the reason.
func (s *Service) keepGuard(ctx context.Context, sess *domain.Session) {
	if s.AgentSystem == nil || s.GuardSessions == nil || sess == nil || sess.ProjectID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	cfg := domain.GuardConfig{Mode: sess.Mode}
	eff, err := s.AgentSystem.Effective(ctx, sess.ProjectID, sess.RepositoryID)
	if err != nil {
		slog.Warn("guard configuration not resolved; the session is not gated", "session", sess.ID, "err", err)
		cfg.Unresolved = err.Error()
	} else {
		cfg = domain.NewGuardConfig(eff, sess.Mode)
	}
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
