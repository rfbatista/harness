package app

import (
	"context"

	"go.uber.org/fx"

	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentboard"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// AgentBoardModule wires the Agent board: its service over the stores, the
// changes it derives its live session feed from, and the project renames it
// follows.
var AgentBoardModule = fx.Module("agentboard",
	fx.Provide(newAgentBoard),
	fx.Invoke(registerAgentBoard),
)

type agentBoardParams struct {
	fx.In

	Sessions ports.SessionRepository
	Tickets  ports.TicketRepository
	Projects ports.ProjectRepository
	Agents   ports.AgentRepository
	Cat      catalog.Catalog
	Orch     *orchestration.Service
}

// newAgentBoard builds the board. Everything it reads comes from the stores
// directly, never through the services, so an observer running inside a
// publisher's call cannot take a lock the publisher holds.
func newAgentBoard(p agentBoardParams) *agentboard.Service {
	return agentboard.New(agentboard.Deps{
		Sessions: p.Sessions, Tickets: p.Tickets, Projects: p.Projects, Agents: p.Agents,
		Asks: p.Orch, Catalog: p.Cat.Projects,
	})
}

// registerAgentBoard has the board see every change on the project feeds, and
// follow project renames from start to stop.
func registerAgentBoard(lc fx.Lifecycle, board *agentboard.Service, orch *orchestration.Service) {
	orch.ObserveChanges(board)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(c context.Context) error {
			if err := board.Prime(c); err != nil {
				return err
			}
			go func() {
				defer close(done)
				board.Run(ctx)
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			<-done
			return nil
		},
	})
}
