package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"operators-mcp/internal/app/runtime"
	"os"
	"path/filepath"

	"go.uber.org/fx"

	"github.com/rfbatista/harnesskit/skillfs"
	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/approval"
	"github.com/rfbatista/llmkit/claude"
	"github.com/rfbatista/llmkit/claude/mcpapprove"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/adapter/out/configrepo"
	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/execution"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra"
	"operators-mcp/internal/ports"

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
		asPort(sqlite.NewEnvFileRepository, new(ports.EnvFileRepository)),
		asPort(sqlite.NewRunCommandRepository, new(ports.RunCommandRepository)),
		asPort(sqlite.NewZoneRepository, new(ports.ZoneRepository)),
		newAgentRepository,
		asPort(sqlite.NewPromptRepository, new(ports.PromptRepository)),
		newSkillRepository,
		newMCPServerRepository,
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

// newSkillRepository binds the database-backed skill store, merged with a
// live config-backed one (agents/skills/*/SKILL.md) when
// Config.DotfilesAgentsDir is set — see internal/adapter/out/configrepo.
func newSkillRepository(cfg Config, db *gorm.DB) ports.SkillRepository {
	dbRepo := sqlite.NewSkillRepository(db)
	if cfg.DotfilesAgentsDir == "" {
		return dbRepo
	}
	cfgRepo := configrepo.NewSkillStore(filepath.Join(cfg.DotfilesAgentsDir, "skills"))
	return configrepo.MergeSkills(dbRepo, cfgRepo)
}

// newMCPServerRepository is newSkillRepository's equivalent for MCP servers,
// read from every agent's resolved .mcp.json.
func newMCPServerRepository(cfg Config, db *gorm.DB) ports.MCPServerRepository {
	dbRepo := sqlite.NewMCPServerRepository(db)
	if cfg.DotfilesAgentsDir == "" {
		return dbRepo
	}
	cfgRepo := configrepo.NewMCPServerStore(filepath.Join(cfg.DotfilesAgentsDir, "agents.json"), cfg.DotfilesAgentsDir)
	return configrepo.MergeMCPServers(dbRepo, cfgRepo)
}

// newAgentRepository is newSkillRepository's equivalent for agents.json's
// "agents" map. It builds its own *configrepo.MCPServerStore rather than
// depend on newMCPServerRepository's merged result: it needs
// serverNamesFor's per-agent resolution, not the flattened, merged list.
func newAgentRepository(cfg Config, db *gorm.DB) ports.AgentRepository {
	dbRepo := sqlite.NewAgentRepository(db)
	if cfg.DotfilesAgentsDir == "" {
		return dbRepo
	}
	manifestPath := filepath.Join(cfg.DotfilesAgentsDir, "agents.json")
	cfgMCP := configrepo.NewMCPServerStore(manifestPath, cfg.DotfilesAgentsDir)
	cfgRepo := configrepo.NewAgentRepository(manifestPath, cfgMCP)
	return configrepo.MergeAgents(dbRepo, cfgRepo)
}

// CatalogModule provides the filesystem adapters (as their ports) and the five
// catalog bounded contexts — projects, architecture, agents, capabilities,
// settings — wired over one event bus by catalog.New.
var CatalogModule = fx.Module("catalog",
	fx.Provide(
		asPort(filesystem.NewMatcher, new(ports.PathMatcher)),
		asPort(filesystem.NewLister, new(ports.TreeLister)),
		asPort(skillfs.NewPublisher, new(ports.SkillPublisher)),
		asPort(gitcli.NewRepositoryFinder, new(ports.RepositoryFinder)),
		asPort(gitcli.NewEnvFiles, new(ports.EnvFileIO)),
		newCatalog,
	),
)

// catalogParams collects the driven ports the catalog contexts are built on.
type catalogParams struct {
	fx.In

	Projects        ports.ProjectRepository
	Repositories    ports.RepositoryRepository
	Zones           ports.ZoneRepository
	BoundedContexts ports.BoundedContextRepository
	Agents          ports.AgentRepository
	Prompts         ports.PromptRepository
	Skills          ports.SkillRepository
	MCPServers      ports.MCPServerRepository
	Tools           ports.ToolRepository
	Settings        ports.SettingsRepository
	Publisher       ports.SkillPublisher
	Matcher         ports.PathMatcher
	Lister          ports.TreeLister
	Finder          ports.RepositoryFinder
	EnvFiles        ports.EnvFileRepository
	EnvFileIO       ports.EnvFileIO
	RunCommands     ports.RunCommandRepository
	Cfg             Config
}

func newCatalog(p catalogParams) catalog.Catalog {
	return catalog.New(catalog.Deps{
		Projects:         p.Projects,
		Repositories:     p.Repositories,
		Zones:            p.Zones,
		BoundedContexts:  p.BoundedContexts,
		Agents:           p.Agents,
		Prompts:          p.Prompts,
		Skills:           p.Skills,
		MCPServers:       p.MCPServers,
		Tools:            p.Tools,
		Settings:         p.Settings,
		Publisher:        p.Publisher,
		PathMatcher:      p.Matcher,
		TreeLister:       p.Lister,
		RepositoryFinder: p.Finder,
		EnvFiles:         p.EnvFiles,
		EnvFileIO:        p.EnvFileIO,
		RunCommands:      p.RunCommands,
		DefaultRoot:      p.Cfg.Root,
	})
}

// PlanningModule provides the planning service (tickets and documents).
var PlanningModule = fx.Module("planning",
	fx.Provide(newPlanningService),
)

// newPlanningService builds the planning service and points its ticket
// announcements at the orchestration, which owns the project feed the board
// and the rail follow.
func newPlanningService(
	tickets ports.TicketRepository,
	documents ports.DocumentRepository,
	projects ports.ProjectRepository,
	orch *orchestration.Service,
) *planning.Service {
	svc := planning.NewService(tickets, documents, projects)
	svc.Announcer = orch
	return svc
}

// ExecutionModule wires the Genkit-backed execution layer: the LLM client, the
// context resolver, the built-in filesystem tools the agent may call, the
// execution service, and flow registration.
var ExecutionModule = fx.Module("execution",
	fx.Provide(
		newGenkit,
		newContextResolver,
		newFilesystemTools,
		execution.NewService,
	),
	fx.Invoke(execution.RegisterFlows),
)

// newContextResolver builds the execution context resolver over the catalog
// contexts it reads.
func newContextResolver(cat catalog.Catalog) *execution.ContextResolver {
	return execution.NewContextResolver(cat.Projects, cat.Architecture, cat.Agents)
}

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

func newToolingService(cat catalog.Catalog, plan *planning.Service, exec *execution.Service) *tooling.Service {
	svc := tooling.NewService()
	tooling.Bootstrap(svc, cat.Tooling(), plan, exec)
	return svc
}

// AgentRuntimeModule wires the Claude CLI runtime: the permission broker, the
// session manager, the event hub, the session repository, and the orchestration
// service — and the terminal host interactive sessions the server runs live
// on. Lifecycle hooks stop all sessions on shutdown, and at boot record as
// stopped the server sessions a previous run left recorded as running.
var AgentRuntimeModule = fx.Module("agentruntime",
	runtime.Module,
	fx.Provide(
		newRuntimeConfig,
		newClaudeManager,
		newBroker,
		newHub,
		asPort(sqlite.NewSessionRepository, new(ports.SessionRepository)),
		asPort(newClaudeTranscripts, new(ports.ClaudeTranscripts)),
		newOrchestrationService,
		newAppsService,
		asPort(sqlite.NewArtifactRepository, new(ports.ArtifactRepository)),
		newArtifactsService,
	),
	fx.Invoke(registerRuntimeShutdown, registerServerSessions),
)

// TaskChannelModule wires the architect channel: its tables, the service,
// the decorators it lends the orchestration and planning, its lifecycle
// subscriptions, and the status-check scheduler, which runs while the server
// does.
var TaskChannelModule = fx.Module("taskchannel",
	fx.Provide(newTaskChannelService),
	fx.Invoke(registerTaskChannel),
)

// newTaskChannelService builds the channel and points the orchestration and
// planning at it: sessions and tickets leave them decorated with their role
// fields, status moves are recorded and announced, and the orchestration's
// role-aware brief asks it for roles.
func newTaskChannelService(db *gorm.DB, sessions ports.SessionRepository, plan *planning.Service, orch *orchestration.Service, art *artifacts.Service, cat catalog.Catalog) *taskchannel.Service {
	svc := taskchannel.New(sessions, plan, taskchannel.Repositories{
		Messages: sqlite.NewTaskMessageRepository(db),
		Reviews:  sqlite.NewReviewRequestRepository(db),
		Checks:   sqlite.NewStatusCheckRepository(db),
	})
	svc.Delivery, svc.Announcer, svc.Feed, svc.Artifacts, svc.Agents = orch, orch, orch, art, cat.Agents
	svc.Subscribe(cat.Bus)
	orch.Roles, orch.Decorator = svc, svc
	plan.Decorator, plan.Events, plan.StatusHistory = svc, cat.Bus, sqlite.NewTaskStatusChangeRepository(db)
	return svc
}

// registerTaskChannel runs the status-check scheduler from start to stop.
func registerTaskChannel(lc fx.Lifecycle, svc *taskchannel.Service) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			svc.Recover()
			go func() { svc.Run(ctx); close(done) }()
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			<-done
			return nil
		},
	})
}

// newArtifactsService records what sessions publish, announces each publish
// on the session's stream through the orchestration, and drops a session's
// task artifacts when the session is deleted. Copies of project artifacts
// live in an artifacts/ directory next to the database; an in-memory
// database gets a temporary one.
func newArtifactsService(cfg Config, repo ports.ArtifactRepository, sessions ports.SessionRepository, orch *orchestration.Service, cat catalog.Catalog) (*artifacts.Service, error) {
	svc := artifacts.NewService(repo, sessions, orch)
	if cfg.DBPath == "" || cfg.DBPath == ":memory:" {
		dir, err := os.MkdirTemp("", "harness-artifacts-")
		if err != nil {
			return nil, err
		}
		svc.StoreDir = dir
	} else {
		svc.StoreDir = filepath.Join(filepath.Dir(cfg.DBPath), "artifacts")
	}
	svc.Subscribe(cat.Bus)
	return svc, nil
}

// newAppsService runs applications from session worktrees on the same
// terminal host the sessions run on, with the repositories' saved commands.
func newAppsService(orch *orchestration.Service, cat catalog.Catalog, host ports.TerminalHost) *apps.Service {
	svc := apps.NewService(orch, cat.Projects, host)
	svc.Subscribe(cat.Bus)
	return svc
}

func newRuntimeConfig(cfg Config) runtime.Config {
	return runtime.Config{ClaudeBin: cfg.ClaudeBin, Shell: cfg.SessionShell}
}

// registerServerSessions reconciles the interactive sessions the server runs
// with its terminal host. At boot none are running, so any recorded as
// running died with the previous server. On shutdown each is recorded as
// stopped before the host kills it, so it does not read as failed. Its hooks
// run inside the host's: the host was built first.
func registerServerSessions(lc fx.Lifecycle, svc *orchestration.Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			n, err := svc.StopOrphanedServerSessions(ctx)
			if n > 0 {
				slog.Info("stopped server sessions left running by a previous run", "count", n)
			}
			return err
		},
		OnStop: func(ctx context.Context) error {
			svc.StopServerSessions(ctx)
			return nil
		},
	})
}

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

func newClaudeTranscripts() claudehome.Transcripts { return claudehome.Transcripts{} }

func newHub() *orchestration.Hub { return orchestration.NewHub(256) }

// newOrchestrationService hands the orchestration the catalog contexts it
// reads to set sessions up.
func newOrchestrationService(
	cfg Config,
	runtime llmkit.Manager,
	broker *approval.Broker,
	hub *orchestration.Hub,
	sessions ports.SessionRepository,
	cat catalog.Catalog,
	tickets ports.TicketRepository,
	ws *workspaces.Service,
	transcripts ports.ClaudeTranscripts,
	terminals ports.TerminalHost,
) *orchestration.Service {
	svc := orchestration.NewService(runtime, broker, hub, sessions, orchestration.Catalog{
		Projects:     cat.Projects,
		Repositories: cat.Projects,
		Agents:       cat.Agents,
		MCPServers:   cat.Capabilities,
		Zones:        cat.Architecture,
	}, tickets, ws)
	base := cfg.loopbackBaseURL()
	// The route belongs to an inbound adapter, so the application layer is
	// handed the URL rather than importing the adapter to build it.
	svc.TaskServerURL = func(sessionID string) string {
		return base + mcpsession.PathPrefix + sessionID
	}
	svc.SessionHookURL = func(sessionID string) string {
		return base + httpapi.InteractiveSessionHookPath + "?session_id=" + url.QueryEscape(sessionID)
	}
	svc.Transcripts = transcripts
	svc.Terminals = terminals
	svc.Events = cat.Bus
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
		asPort(gitcli.NewHistory, new(ports.GitHistory)),
		newWorkspacesService,
	),
)

func newWorkspacesService(
	ws ports.WorkspaceRepository,
	repos ports.RepositoryRepository,
	wt ports.WorktreeManager,
	settings ports.SettingsRepository,
	envFiles ports.EnvFileRepository,
	envIO ports.EnvFileIO,
	history ports.GitHistory,
) *workspaces.Service {
	svc := workspaces.NewService(ws, repos, wt, settings)
	svc.UseEnvFiles(envFiles, envIO)
	svc.UseHistory(history)
	return svc
}
