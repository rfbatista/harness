package app

import (
	"context"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// newAgentSystem wires the agent system settings store (ADR 0011): its
// SQLite rows, the review-mode model as the critic's default, and the event
// bus and project feed that announce a save.
func newAgentSystem(db *gorm.DB, cat catalog.Catalog, orch *orchestration.Service) *agentsystem.Service {
	svc := agentsystem.New(agentsystem.Deps{
		Store:        sqlite.NewAgentSystemRepository(db),
		Projects:     projectLookup{cat},
		Repositories: repositoryLookup{cat},
		ReviewModel: func() string {
			return domain.ModeModelOf(domain.SessionModeReview, cat.Settings.Setting)
		},
		Events: cat.Bus,
		Feed:   orch,
	})
	svc.Subscribe(cat.Bus)
	return svc
}

type projectLookup struct{ cat catalog.Catalog }

func (l projectLookup) Get(ctx context.Context, id string) *domain.Project {
	p, err := l.cat.Projects.GetProject(ctx, id)
	if err != nil {
		return nil
	}
	return p
}

type repositoryLookup struct{ cat catalog.Catalog }

func (l repositoryLookup) Get(ctx context.Context, id string) *domain.Repository {
	r, err := l.cat.Projects.GetRepository(ctx, id)
	if err != nil {
		return nil
	}
	return r
}
