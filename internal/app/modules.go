package app

import (
	"context"
	"fmt"
	"io"
	"os"

	"go.uber.org/fx"

	"github.com/rfbatista/harnesskit/skillfs"
	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/approval"
	"github.com/rfbatista/llmkit/claude"
	"github.com/rfbatista/llmkit/claude/mcpapprove"

	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/application/execution"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra"

	"github.com/firebase/genkit/go/genkit"
	"gorm.io/gorm"
)

// PersistenceModule opens the SQLite connection and exposes every repository
// bound to its outbound port interface. The connection is closed on shutdown
// via a lifecycle hook.
var PersistenceModule = fx.Module("persistence",
	fx.Provide(
		newDB,
		asPort(sqlite.NewProjectRepository, new(ports.ProjectRepository)),
		asPort(sqlite.NewRepositoryRepository, new(ports.RepositoryRepository)),
		asPort(sqlite.NewZoneRepository, new(ports.ZoneRepository)),
		asPort(sqlite.NewAgentRepository, new(ports.AgentRepository)),
		asPort(sqlite.NewPromptRepository, new(ports.PromptRepository)),
		asPort(sqlite.NewSkillRepository, new(ports.SkillRepository)),
		asPort(sqlite.NewMCPServerRepository, new(ports.MCPServerRepository)),
		asPort(sqlite.NewToolRepository, new(ports.ToolRepository)),
		asPort(sqlite.NewTaskRepository, new(ports.TaskRepository)),
		asPort(sqlite.NewTicketRepository, new(ports.TicketRepository)),
		asPort(sqlite.NewDocumentRepository, new(ports.DocumentRepository)),
		asPort(sqlite.NewSettingsRepository, new(ports.SettingsRepository)),
		asPort(sqlite.NewWorkspaceRepository, new(ports.WorkspaceRepository)),
		asPort(sqlite.NewBoundedContextRepository, new(ports.BoundedContextRepository)),
	),
)

// asPort annotates a concrete repository constructor so fx exposes it as the
// given port interface rather than the concrete type.
func asPort(ctor any, iface any) any {
	return fx.Annotate(ctor, fx.As(iface))
}

// newDB opens the SQLite database and registers a hook to close it on shutdown.
func newDB(lc fx.Lifecycle, cfg Config) (*gorm.DB, error) {
	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			sqlDB, err := db.DB()
			if err != nil {
				return err
			}
			return sqlDB.Close()
		},
	})
	return db, nil
}

// BlueprintModule provides the filesystem adapters (as their ports) and the
// blueprint domain service that ties the repositories together.
var BlueprintModule = fx.Module("blueprint",
	fx.Provide(
		asPort(filesystem.NewMatcher, new(ports.PathMatcher)),
		asPort(filesystem.NewLister, new(ports.TreeLister)),
		asPort(skillfs.NewPublisher, new(ports.SkillPublisher)),
		newBlueprintService,
	),
)

// blueprintParams collects the many dependencies of blueprint.NewService into
// a parameter object so the wiring stays readable.
type blueprintParams struct {
	fx.In

	Projects     ports.ProjectRepository
	Repositories ports.RepositoryRepository
	Zones        ports.ZoneRepository
	Agents       ports.AgentRepository
	Prompts      ports.PromptRepository
	Skills       ports.SkillRepository
	MCPServers   ports.MCPServerRepository
	Tools        ports.ToolRepository
	Matcher      ports.PathMatcher
	Lister       ports.TreeLister
	Cfg          Config
	Settings     ports.SettingsRepository
	Publisher    ports.SkillPublisher

	BoundedContexts ports.BoundedContextRepository
}

func newBlueprintService(p blueprintParams) *blueprint.Service {
	return blueprint.NewService(
		p.Projects, p.Repositories, p.Zones, p.Agents, p.Prompts, p.Skills, p.MCPServers, p.Tools,
		p.Matcher, p.Lister, p.Cfg.Root,
	).WithPublishing(p.Settings, p.Publisher).WithBoundedContexts(p.BoundedContexts)
}

// PlanningModule provides the planning service (tickets and documents).
var PlanningModule = fx.Module("planning",
	fx.Provide(newPlanningService),
)

func newPlanningService(
	tickets ports.TicketRepository,
	documents ports.DocumentRepository,
	projects ports.ProjectRepository,
) *planning.Service {
	return planning.NewService(tickets, documents, projects)
}

// ExecutionModule wires the Genkit-backed execution layer: the LLM client, the
// context resolver, the built-in filesystem tools the agent may call, the
// execution service, and flow registration.
var ExecutionModule = fx.Module("execution",
	fx.Provide(
		newGenkit,
		execution.NewContextResolver,
		newFilesystemTools,
		execution.NewService,
	),
	fx.Invoke(execution.RegisterFlows),
)

// newGenkit returns the singleton Genkit instance. Initialization uses a
// background context because the instance lives for the whole process.
func newGenkit() *genkit.Genkit {
	return infra.GenkitInstance(context.Background())
}

// newFilesystemTools provides the built-in filesystem tools consumed by the
// execution service.
func newFilesystemTools() []domain.Tool {
	return tooling.FilesystemTools()
}

// ToolingModule provides the tool registry, fully populated by Bootstrap so
// every consumer (HTTP API, MCP server) sees the complete tool set.
var ToolingModule = fx.Module("tooling",
	fx.Provide(newToolingService),
)

func newToolingService(bp *blueprint.Service, plan *planning.Service, exec *execution.Service) *tooling.Service {
	svc := tooling.NewService()
	tooling.Bootstrap(svc, bp, plan, exec)
	return svc
}

// AgentRuntimeModule wires the Claude CLI runtime: the permission broker, the
// session manager, the event hub, the session repository, and the orchestration
// service. A lifecycle hook stops all sessions on shutdown.
var AgentRuntimeModule = fx.Module("agentruntime",
	fx.Provide(
		newClaudeManager,
		newBroker,
		newHub,
		asPort(sqlite.NewSessionRepository, new(ports.SessionRepository)),
		newOrchestrationService,
	),
	fx.Invoke(registerRuntimeShutdown),
)

func newClaudeManager(cfg Config) llmkit.Manager {
	base := cfg.loopbackBaseURL()
	return claude.New(llmkit.Options{
		Bin: cfg.ClaudeBin,
		ApprovalURL: func(sessionID string) string {
			return base + mcpapprove.PathPrefix + sessionID
		},
		ApprovalTimeout: cfg.ApprovalTimeout,
		RawLog:          rawLogWriter(cfg),
	})
}

// rawLogWriter tees each session's raw stdout to the terminal, or nil to keep
// the console quiet.
func rawLogWriter(cfg Config) io.Writer {
	if !cfg.ClaudeLogStdout {
		return nil
	}
	return os.Stdout
}

// newBroker takes the approval broker off the manager. The driver owns it,
// because the driver is what knows which of its tools must never be
// auto-answered.
func newBroker(m llmkit.Manager) (*approval.Broker, error) {
	gated, ok := m.(llmkit.Gated)
	if !ok {
		return nil, fmt.Errorf("app: manager %T does not implement llmkit.Gated", m)
	}
	return gated.Approvals(), nil
}

func newHub() *orchestration.Hub { return orchestration.NewHub(256) }

// newOrchestrationService passes *blueprint.Service where the orchestration's
// configResolver interface is expected (structural satisfaction).
func newOrchestrationService(
	cfg Config,
	runtime llmkit.Manager,
	broker *approval.Broker,
	hub *orchestration.Hub,
	sessions ports.SessionRepository,
	bp *blueprint.Service,
	tickets ports.TicketRepository,
	ws *workspaces.Service,
) *orchestration.Service {
	svc := orchestration.NewService(runtime, broker, hub, sessions, bp, tickets, ws)
	base := cfg.loopbackBaseURL()
	// The route belongs to an inbound adapter, so the application layer is
	// handed the URL rather than importing the adapter to build it.
	svc.TaskServerURL = func(sessionID string) string {
		return base + mcpsession.PathPrefix + sessionID
	}
	return svc
}

// TextProcessingModule binds the one-shot Claude CLI adapters to the text
// processing ports. Each port gets its own adapter so a consumer depends on the
// single capability it uses rather than on "the text processor".
var TextProcessingModule = fx.Module("textprocessing",
	fx.Provide(
		asPort(newSummarizer, new(ports.Summarizer)),
		asPort(newTextGenerator, new(ports.TextGenerator)),
		asPort(newTranslator, new(ports.Translator)),
	),
)

func newSummarizer(cfg Config) *claudetext.Summarizer {
	return claudetext.NewSummarizer(cfg.ClaudeBin, cfg.ClaudeTextModel)
}

func newTextGenerator(cfg Config) *claudetext.TextGenerator {
	return claudetext.NewTextGenerator(cfg.ClaudeBin, cfg.ClaudeTextModel)
}

func newTranslator(cfg Config) *claudetext.Translator {
	return claudetext.NewTranslator(cfg.ClaudeBin, cfg.ClaudeTextModel)
}

func registerRuntimeShutdown(lc fx.Lifecycle, m llmkit.Manager) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			m.StopAll()
			return nil
		},
	})
}

// WorkspacesModule provides the git worktree adapter and the workspaces
// service (isolated worktrees of a repository, tracked in the database).
var WorkspacesModule = fx.Module("workspaces",
	fx.Provide(
		asPort(gitcli.NewWorktreeManager, new(ports.WorktreeManager)),
		newWorkspacesService,
	),
)

func newWorkspacesService(
	ws ports.WorkspaceRepository,
	repos ports.RepositoryRepository,
	wt ports.WorktreeManager,
	settings ports.SettingsRepository,
) *workspaces.Service {
	return workspaces.NewService(ws, repos, wt, settings)
}
