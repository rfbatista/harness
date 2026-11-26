package app

import (
	"context"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/application/critic"
	"operators-mcp/internal/application/gateruns"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/visualqa"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/ports"
)

// newCritic is the Review layer's critic launcher (ADR 0011): it watches the
// project feed for a finished gate run and starts the critic session.
func newCritic(sessions ports.SessionRepository, notes *sqlite.SessionRepository, settings *agentsystem.Service, gate *gateruns.Service, reviews *sqlite.CriticReviewRepository, plan *planning.Service, orch *orchestration.Service, ws *workspaces.Service) *critic.Service {
	return critic.New(critic.Deps{
		Sessions: sessions, Settings: settings, Gated: gate, Reviews: reviews, Documents: plan, Starter: orch, Notes: notes, Announce: orch,
		Diff: critic.GitDiff{BaseSHA: func(ctx context.Context, workspaceID string) string {
			if w := ws.Get(ctx, workspaceID); w != nil {
				return w.BaseSHA
			}
			return ""
		}},
	})
}

// registerCritic has the critic see every change on the project feeds, and ends
// its launches in flight when the server stops, before the stores close.
func registerCritic(lc fx.Lifecycle, c *critic.Service, orch *orchestration.Service) {
	orch.ObserveChanges(c)
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		c.Stop(ctx)
		return nil
	}})
}

// newVisualQA starts visual QA for a session on the repository's declared UI.
func newVisualQA(sessions ports.SessionRepository, settings *agentsystem.Service, orch *orchestration.Service) *visualqa.Service {
	return visualqa.New(visualqa.Deps{Sessions: sessions, Settings: settings, Starter: orch})
}
