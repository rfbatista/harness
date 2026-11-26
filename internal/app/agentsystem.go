package app

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/mcpbench"
	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/app/runtime"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// newAgentSystem wires the agent system settings store (ADR 0011): its
// SQLite rows, the review-mode model as the critic's default, and the event
// bus and project feed that announce a save.
func newAgentSystem(db *gorm.DB, cat catalog.Catalog, orch *orchestration.Service, git ports.ArchitectureGit) *agentsystem.Service {
	svc := agentsystem.New(agentsystem.Deps{
		Store:        sqlite.NewAgentSystemRepository(db),
		Projects:     projectLookup{cat},
		Repositories: repositoryLookup{cat},
		ReviewModel: func() string {
			return domain.ModeModelOf(domain.SessionModeReview, cat.Settings.Setting)
		},
		Detector: agentsystem.NewDetector(git, exec.LookPath),
		Events:   cat.Bus,
		Feed:     orch,
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

// newBench wires the eval bench (ADR 0015): its cases in SQLite, checked
// against the project catalog, and the orchestration service as the one that
// runs the CLI for a case on a manager of its own with no approval, interview
// or task server, so a run's only MCP server is the mock.
func newBench(cfg Config, db *gorm.DB, cat catalog.Catalog, orch *orchestration.Service, bin ports.ManagedClaudeBin, home claudehome.Home, settings *agentsystem.Service, tickets ports.TicketRepository, sessions ports.SessionRepository) *bench.Service {
	claudeMgr := oneShotClaude(bin.Bin(), home.Path(), cfg.llmProxyURL())
	orch.BenchRuntime = claudeMgr
	orch.BenchCLIVersion = cliVersion(bin)
	orch.BenchSandboxAllow = sandboxAllowlist(allowSources{ClaudeDirs: runtime.ClaudeInstallDirs(bin.Bin())})
	svc := bench.New(bench.Deps{
		Cases:      sqlite.NewBenchCaseRepository(db),
		Scorecards: sqlite.NewBenchScorecardRepository(db),
		Projects:   projectLookup{cat},
		Sessions:   orch,
		// The judge is one tool-less turn on the critic model; it does not run
		// under the sandbox a session does.
		Judge:     claudetext.NewModelAsker(claudeMgr, bin.Bin()),
		ServeMock: mcpbench.Serve,
		MockName:  mcpbench.Name,
		// A run with no model of its own uses the one sessions run on.
		SessionModel: func() string {
			return domain.ModeModelOf(domain.SessionModeImplement, cat.Settings.Setting)
		},
		// The judge model is the project's effective critic model.
		Settings: settings,
		Feed:     orch,
		// Trajectory recording: a session that ends on a recorded task is read
		// back from its stored events.
		Trajectories: sqlite.NewBenchTrajectoryRepository(db),
		Tasks:        tickets,
		SessionLog:   sessions,
	})
	svc.Subscribe(cat.Bus)
	return svc
}

// cliVersion asks the claude CLI for its version each time a bench run starts,
// the binary that run launches (the managed one moves when it is updated);
// "" when it cannot say.
func cliVersion(bin ports.ManagedClaudeBin) func() string {
	return func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin.Bin(), "--version").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
}
