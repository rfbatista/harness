// Package critic launches the critic: after a green gate run of a gated
// session the harness starts a review-mode session with a fresh context,
// read-only on the author's worktree, on the critic model of the effective
// configuration (Glossary "Critic"; ADR 0011). It is advisory and one pass per
// diff.
package critic

import (
	"context"
	"log/slog"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ProjectChangeObserver = (*Service)(nil)

// Deps are what the critic's launch reads and drives.
type Deps struct {
	Sessions interface {
		Find(ctx context.Context, id string) (*domain.Session, error)
	}
	// Settings gives the effective configuration of the author's repository.
	Settings ports.AgentSystemSettings
	// Gated answers nil error for a gated session (the gate runner's check).
	Gated interface {
		Gated(ctx context.Context, sessionID string) (*domain.Session, domain.GuardConfig, error)
	}
	Reviews ports.CriticReviewStore
	// Diff reads the author's branch.
	Diff DiffSource
	// Documents lists the task's documents; nil: the brief names none.
	Documents ports.TicketDocumentReader
	// Starter starts the critic session.
	Starter interface {
		StartInteractive(ctx context.Context, req ports.InteractiveRequest) (*domain.Session, ports.AgentSpec, error)
	}
}

// DiffSource reads the author's branch against the commit it was cut at.
type DiffSource interface {
	// Range is the base and head commits of the session's branch.
	Range(ctx context.Context, sess *domain.Session) (base, head string, err error)
	// Diff is the session's diff against base.
	Diff(ctx context.Context, sess *domain.Session, base string) (string, error)
	// File reads a file of the session's worktree; "" when it has none.
	File(ctx context.Context, sess *domain.Session, path string) string
}

// Service is the critic's launcher.
type Service struct{ d Deps }

// New returns the launcher.
func New(d Deps) *Service { return &Service{d: d} }

// ProjectChanged launches the critic for a finished gate run. It is told from
// the publisher's goroutine, so the work runs apart from it.
func (s *Service) ProjectChanged(change ports.ProjectChange) {
	if change.GateRun == nil || change.GateRun.Phase != "finished" {
		return
	}
	run := *change.GateRun
	go func() {
		if _, err := s.Handle(context.Background(), run); err != nil {
			slog.Warn("critic launch failed", "session", run.SessionID, "run", run.RunID, "err", err)
		}
	}()
}

// Handle decides on a gate run change and launches the critic when it says
// so. It answers the critic session, nil when none was launched. A failure is
// the caller's to log: it never reaches the author's done declaration.
func (s *Service) Handle(ctx context.Context, run ports.GateRunChange) (*domain.Session, error) {
	in := domain.CriticLaunchInput{Phase: run.Phase, Result: run.Result, Trigger: run.Trigger}
	// The cheap checks first: no store is read for a run that cannot launch.
	if domain.DecideCritic(withEnabled(in, true, true)) != domain.CriticLaunches {
		return nil, nil
	}
	author, _, err := s.d.Gated.Gated(ctx, run.SessionID)
	if err != nil {
		return nil, nil // not gated: nothing to review
	}
	eff, err := s.d.Settings.Effective(ctx, author.ProjectID, author.RepositoryID)
	if err != nil {
		return nil, err
	}
	in.Gated, in.Enabled = true, eff.Review.Critic.Enabled.Value
	base, head, err := s.d.Diff.Range(ctx, author)
	if err != nil {
		return nil, err
	}
	if in.DiffSeen, err = s.d.Reviews.Seen(ctx, author.ID, base, head); err != nil {
		return nil, err
	}
	if domain.DecideCritic(in) != domain.CriticLaunches {
		return nil, nil
	}
	// Two runs finishing together on one diff launch one critic.
	if claimed, err := s.d.Reviews.Claim(ctx, author.ID, base, head, run.RunID); err != nil || !claimed {
		return nil, err
	}
	release := func() { _ = s.d.Reviews.Release(context.WithoutCancel(ctx), author.ID, base, head) }
	diff, err := s.d.Diff.Diff(ctx, author, base)
	if err != nil {
		release()
		return nil, err
	}
	brief := s.brief(ctx, author, eff, base, head, diff)
	crit, _, err := s.d.Starter.StartInteractive(ctx, ports.InteractiveRequest{
		ProjectID:    author.ProjectID,
		RepositoryID: author.RepositoryID,
		TicketID:     author.TicketID,
		Mode:         string(domain.SessionModeReview),
		Model:        strings.TrimSpace(eff.Review.Critic.Model.Value),
		Prompt:       brief,
		RunsOn:       domain.RunnerServer,
		WorktreeID:   author.WorkspaceID,
		Access:       string(domain.AccessReadOnly),
		// Nobody answers a permission prompt: the read-only worktree is what
		// keeps it from writing.
		AutoAccept: "all",
		Critic:     &ports.CriticLink{AuthorSessionID: author.ID, GateRunID: run.RunID},
	})
	if err != nil {
		release()
		return nil, err
	}
	return crit, nil
}

// withEnabled sets the two inputs the cheap pre-check does not know.
func withEnabled(in domain.CriticLaunchInput, enabled, gated bool) domain.CriticLaunchInput {
	in.Enabled, in.Gated = enabled, gated
	return in
}
