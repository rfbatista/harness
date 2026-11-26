package app

import (
	"context"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/application/critic"
	"operators-mcp/internal/application/gateruns"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/ports"
)

// newCritic is the Review layer's critic launcher (ADR 0011): it watches the
// project feed for a finished gate run and starts the critic session.
func newCritic(sessions ports.SessionRepository, settings *agentsystem.Service, gate *gateruns.Service, reviews *sqlite.CriticReviewRepository, plan *planning.Service, orch *orchestration.Service, ws *workspaces.Service) *critic.Service {
	return critic.New(critic.Deps{
		Sessions: sessions, Settings: settings, Gated: gate, Reviews: reviews, Documents: plan, Starter: orch,
		Diff: critic.GitDiff{BaseSHA: func(ctx context.Context, workspaceID string) string {
			if w := ws.Get(ctx, workspaceID); w != nil {
				return w.BaseSHA
			}
			return ""
		}},
	})
}

func registerCritic(c *critic.Service, orch *orchestration.Service) { orch.ObserveChanges(c) }
