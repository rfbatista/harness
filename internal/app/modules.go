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
	"operators-mcp/internal/adapter/out/blobstore"
	"operators-mcp/internal/adapter/out/configrepo"
	"operators-mcp/internal/adapter/out/developerdir"
	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/githubapi"
	"operators-mcp/internal/adapter/out/keychainshim"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/archrules"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/execution"
	"operators-mcp/internal/application/gitcredentials"
	"operators-mcp/internal/application/keychainpath"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/application/writinghelper"
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
		asPort(sqlite.NewActivationRepository, new(ports.TokenSaverActivationRepository)),
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
		asPort(sqlite.NewPullRequestRepository, new(ports.PullRequestRepository)),
		asPort(sqlite.NewArchitectureRecordRepository, new(ports.ArchitectureRecordStore)),
		asPort(sqlite.NewArchitectureRuleEventRepository, new(ports.ArchitectureRuleEventStore)),
	),
)

// asPort annotates a concrete repository constructor so fx exposes it as the
// given port interface rather than the concrete type.
func asPort(ctor any, iface any) any {
	return fx.Annotate(ctor, fx.As(iface))
}

// gitOptions are the git adapters' options for c's git binary and deadlines.
func (c Config) gitOptions() []gitcli.Option {
	return []gitcli.Option{gitcli.WithGit(c.GitBin), gitcli.WithWorktreeDeadline(c.GitWorktreeDeadline), gitcli.WithReadDeadline(c.GitReadDeadline)}
}

// The git adapters run cfg's git, each run bound by cfg's deadlines (see
// gitcli.Deadlines).
func newRepositoryFinder(cfg Config) *gitcli.RepositoryFinder {
	return gitcli.NewRepositoryFinder(cfg.gitOptions()...)
}
func newWorktreeManager(cfg Config) *gitcli.WorktreeManager {
	return gitcli.NewWorktreeManager(cfg.gitOptions()...)
}
func newArchitectureGit(cfg Config) *gitcli.Architecture {
	return gitcli.NewArchitecture(cfg.gitOptions()...)
}
func newCommitGuard(cfg Config) *gitcli.CommitGuard {
	return gitcli.NewCommitGuard(cfg.gitOptions()...)
}
func newEnvFiles(cfg Config) *gitcli.EnvFiles  { return gitcli.NewEnvFiles(cfg.gitOptions()...) }
func newGitHistory(cfg Config) *gitcli.History { return gitcli.NewHistory(cfg.gitOptions()...) }
func newBranchPublisher(cfg Config) *gitcli.BranchPublisher {
	return gitcli.NewBranchPublisher(cfg.gitOptions()...)
}
func newGitMerger(cfg Config) *gitcli.Merger { return gitcli.NewMerger(cfg.gitOptions()...) }
func newGitBranchNames(cfg Config) *gitcli.BranchNames {
	return gitcli.NewBranchNames(cfg.gitOptions()...)
}
func newGitCode(cfg Config) *gitcli.Code { return gitcli.NewCode(cfg.gitOptions()...) }

// newDB opens the SQLite database and registers a hook to close it on
// shutdown. The database's folder is made first: the default data dir,
// ~/.harness, may not exist yet.
func newDB(lc fx.Lifecycle, cfg Config) (*gorm.DB, error) {
	if !cfg.inMemory() {
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
			return nil, fmt.Errorf("make the database's folder: %w", err)
		}
	}
	db, err := sqlite.Open(cfg.DBPath, sqlite.WithSlowThreshold(cfg.DBSlowThreshold))
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error { return sqlite.Close(db) },
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
		asPort(newRepositoryFinder, new(ports.RepositoryFinder)),
		asPort(newEnvFiles, new(ports.EnvFileIO)),
		asPort(newCommitGuard, new(ports.CommitGuard)),
		asPort(newArchitectureGit, new(ports.ArchitectureGit)),
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
	ArchRecords     ports.ArchitectureRecordStore
	ArchGit         ports.ArchitectureGit
	ArchEvents      ports.ArchitectureRuleEventStore
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
	Activations     ports.TokenSaverActivationRepository
	Cfg             Config
}

func newCatalog(p catalogParams) catalog.Catalog {
	return catalog.New(catalog.Deps{
		Projects:         p.Projects,
		Repositories:     p.Repositories,
		Zones:            p.Zones,
		BoundedContexts:  p.BoundedContexts,
		ArchRecords:      p.ArchRecords,
		ArchGit:          p.ArchGit,
		ArchEvents:       p.ArchEvents,
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
		Activations:      p.Activations,
		DefaultRoot:      p.Cfg.Root,
	})
}

// PlanningModule provides the planning service (tickets and documents).
var PlanningModule = fx.Module("planning",
	fx.Provide(newPlanningService),
	fx.Invoke(bindProjectActivity),
)

// bindProjectActivity lets the projects context read its projects' tasks and
// sessions (summaries, the delete guard), and capabilities a task-level
// activation's task. The catalog is built before planning and
// orchestration, so the read ports are bound late.
func bindProjectActivity(cat catalog.Catalog, plan *planning.Service, orch *orchestration.Service) {
	cat.Projects.UseActivity(plan, orch)
	cat.Capabilities.UseTasks(plan)
}

// newPlanningService builds the planning service and points its ticket,
// resource and comment announcements at the orchestration, which owns the
// project feed the board and the rail follow. A person's additions to a task
// go on the bus too, and the orchestration tells the task's running sessions. The
// bytes of uploaded resources live in a task-resources/ directory next to
// the database; an in-memory database gets a temporary one.
func newPlanningService(
	cfg Config,
	db *gorm.DB,
	tickets ports.TicketRepository,
	documents ports.DocumentRepository,
	projects ports.ProjectRepository,
	orch *orchestration.Service,
	cat catalog.Catalog,
) (*planning.Service, error) {
	svc := planning.NewService(tickets, documents, projects)
	svc.Announcer, svc.Feed, svc.Events = orch, orch, cat.Bus
	dir, err := dataDir(cfg, "task-resources")
	if err != nil {
		return nil, err
	}
	svc.Resources, svc.Comments = sqlite.NewTicketResourceRepository(db), sqlite.NewTicketCommentRepository(db)
	svc.Blobs, svc.ContentURL = blobstore.New(dir), httpapi.TicketResourceContentPath
	// A session's brief counts them, and its task's additions become turns.
	orch.Resources, orch.Comments = svc, svc
	orch.SubscribeTaskUpdates(cat.Bus)
	return svc, nil
}

// dataDir is name in the data dir (Config.DataDir, or the database's
// folder), or a fresh temporary directory when there is none. It is always
// absolute: token savers installed under it run from checkouts and
// worktrees, where a relative path points nowhere.
func dataDir(cfg Config, name string) (string, error) {
	root, err := cfg.dataRoot()
	if err != nil {
		return "", err
	}
	if root == "" {
		return os.MkdirTemp("", "harness-"+name+"-")
	}
	return filepath.Join(root, name), nil
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

func newToolingService(cat catalog.Catalog, plan *planning.Service, exec *execution.Service, inbox optionalNotifications) *tooling.Service {
	svc := tooling.NewService()
	tooling.Bootstrap(svc, cat.Tooling(), plan, exec)
	if inbox.Service != nil {
		svc.Register(tooling.NotificationTools(inbox.Service)...)
	}
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
		sqlite.NewSessionRepository,
		func(r *sqlite.SessionRepository) ports.SessionRepository { return r },
		func(r *sqlite.SessionRepository) ports.SandboxViolationStore { return r },
		newAgentClaudeHome,
		asPort(newClaudeTranscripts, new(ports.ClaudeTranscripts)),
		asPort(newClaudeTranscriptReader, new(ports.ClaudeTranscriptReader)),
		newOrchestrationService,
		newArchitectureRules,
		platformSandboxDriver,
		platformSandboxViolations,
		newSandboxSettings,
		newManagedClaude,
		newManagedClaudeBin,
		newAgentReadiness,
		newAppsService,
		asPort(sqlite.NewArtifactRepository, new(ports.ArtifactRepository)),
		newArtifactsService,
		newVirtualShells,
	),
	fx.Invoke(registerRuntimeShutdown, registerServerSessions, bindVirtualShells, bindSandboxSettings,
		bindAgentReadiness, func(*archrules.Service) {}),
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
// role-aware brief asks it for roles. Tickets also carry their merge state.
func newTaskChannelService(db *gorm.DB, sessions ports.SessionRepository, plan *planning.Service, orch *orchestration.Service, art *artifacts.Service, cat catalog.Catalog, merges *workspaces.MergeStates) *taskchannel.Service {
	svc := taskchannel.New(sessions, plan, taskchannel.Repositories{
		Messages: sqlite.NewTaskMessageRepository(db),
		Reviews:  sqlite.NewReviewRequestRepository(db),
		Checks:   sqlite.NewStatusCheckRepository(db),
	})
	svc.Delivery, svc.Announcer, svc.Feed, svc.Artifacts, svc.Agents = orch, orch, orch, art, cat.Agents
	svc.Subscribe(cat.Bus)
	orch.Roles, orch.Decorator = svc, svc
	plan.Decorator = ports.TicketDecorators{svc, merges}
	plan.MergeRefresher = merges
	plan.StatusHistory = sqlite.NewTaskStatusChangeRepository(db) // plan.Events is the bus already
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
// on the session's stream and each project-asset change on the project feed
// through the orchestration, attaches project assets to the tasks planning
// knows (and lends the orchestration its reader, for the task brief), and drops a session's task artifacts when the session is deleted
// and a task's attachments when the task is. Copies of project artifacts
// live in an artifacts/ directory next to the database; an in-memory
// database gets a temporary one.
func newArtifactsService(cfg Config, repo ports.ArtifactRepository, sessions ports.SessionRepository, orch *orchestration.Service, plan *planning.Service, cat catalog.Catalog) (*artifacts.Service, error) {
	svc := artifacts.NewService(repo, sessions, orch)
	svc.Tickets, svc.Feed, svc.Events = plan, orch, cat.Bus
	orch.Artifacts = svc // a session's brief lists its task's attached assets
	dir, err := dataDir(cfg, "artifacts")
	if err != nil {
		return nil, err
	}
	svc.StoreDir = dir
	svc.Subscribe(cat.Bus)
	return svc, nil
}

// newVirtualShells fills task sessions' virtual shells from planning's
// tickets, documents, comments and resources and the artifacts' designs,
// and stores the documents and comments agents write in them through
// planning and their designs through the artifacts.
func newVirtualShells(plan *planning.Service, art *artifacts.Service, sessions ports.SessionRepository, cat catalog.Catalog) *vshell.Service {
	src := vshell.Sources{Tickets: plan, Documents: plan, Comments: plan, Resources: plan, Artifacts: art, Writes: plan,
		Designs: art, Sessions: sessions, Agents: cat.Agents, Settings: cat.Settings}
	if cat.RepositoryArchitectures != nil {
		src.Architectures = cat.RepositoryArchitectures
	}
	return vshell.New(src)
}

// bindVirtualShells gives every session with a task its virtual shell, and
// has the shells hear every change they may have to show: documents' and
// artifacts' on the bus, comments' and resources' as planning puts them on
// the project feed.
func bindVirtualShells(orch *orchestration.Service, shells *vshell.Service, plan *planning.Service, cat catalog.Catalog) {
	orch.Shells, orch.ShellIndex = shells, shells
	shells.Subscribe(cat.Bus)
	plan.Feed = changeSinks{plan.Feed, shells}
	plan.Announcer = ticketAnnouncers{plan.Announcer, shells}
	// A saved architecture reaches the shells as well as the project feed.
	if cat.RepositoryArchitectures != nil {
		cat.RepositoryArchitectures.UseFeed(changeSinks{orch, shells})
	}
}

// ticketAnnouncers tells every announcer of each task change in turn.
type ticketAnnouncers []ports.TicketAnnouncer

func (a ticketAnnouncers) AnnounceTicket(tk *domain.Ticket, deleted bool) {
	for _, to := range a {
		if to != nil {
			to.AnnounceTicket(tk, deleted)
		}
	}
}

// changeSinks puts each change on every sink in turn.
type changeSinks []ports.ProjectChangeSink

func (s changeSinks) AnnounceChange(c ports.ProjectChange) {
	for _, sink := range s {
		if sink != nil {
			sink.AnnounceChange(c)
		}
	}
}

// newAppsService runs applications from session worktrees on the same
// terminal host the sessions run on, with the repositories' saved commands,
// refreshing their env files in the worktree before each run.
func newAppsService(orch *orchestration.Service, cat catalog.Catalog, host ports.TerminalHost, envIO ports.EnvFileIO) *apps.Service {
	svc := apps.NewService(orch, cat.Projects, host)
	svc.UseEnvFiles(cat.Projects, envIO)
	svc.Subscribe(cat.Bus)
	return svc
}

func newRuntimeConfig(cfg Config, bin ports.ManagedClaudeBin, home claudehome.Home, driver ports.SandboxDriver) runtime.Config {
	return runtime.Config{
		ClaudeBin: bin.Bin(), Shell: cfg.SessionShell, ProxyURL: cfg.llmProxyURL(),
		ConfigDir: home.Path(), Sandbox: driver,
	}
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

// newClaudeManager is the headless runtime: every agent claude runs from the
// managed install (or the -claude.bin override) in the agent Claude home,
// isolated from the person's own configuration (ADR 0018).
func newClaudeManager(cfg Config, bin ports.ManagedClaudeBin, home claudehome.Home) llmkit.Manager {
	base := cfg.loopbackBaseURL()
	return claude.New(llmkit.Options{
		Bin:       bin.Bin(),
		ConfigDir: home.Path(),
		Isolated:  true,
		ApprovalURL: func(sessionID string) string {
			return base + mcpapprove.PathPrefix + sessionID
		},
		ApprovalTimeout: cfg.ApprovalTimeout,
		RawLog:          rawLogWriter(cfg),
		ProxyURL:        cfg.llmProxyURL(),
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

// agentClaudeHomeDir is the agent Claude home in the data dir.
func agentClaudeHomeDir(cfg Config) (string, error) { return dataDir(cfg, "claude-home") }

// newAgentClaudeHome creates and seeds the agent Claude home, so an
// interactive claude started with its path as CLAUDE_CONFIG_DIR meets no
// first-run prompt.
func newAgentClaudeHome(cfg Config) (claudehome.Home, error) {
	dir, err := agentClaudeHomeDir(cfg)
	if err != nil {
		return claudehome.Home{}, err
	}
	h := claudehome.NewHome(dir)
	return h, h.Ensure()
}

// agentHomeExposed checks once, now, whether the agent Claude home lies in a
// git work tree that does not ignore it, logs a warning if so, and returns
// the answer for the Agent isolation page.
func agentHomeExposed(cfg Config) func() bool {
	exposed := false
	if dir, err := agentClaudeHomeDir(cfg); err == nil {
		exposed = gitcli.HomeExposedToGit(cfg.GitBin, dir)
		if exposed {
			slog.Warn("the agent home is inside a git work tree and not ignored: add it to .gitignore", "dir", dir)
		}
	}
	return func() bool { return exposed }
}

// agentHasLogin says, at each call, whether the agent Claude home holds a
// Claude login: the file is read, no process is started.
func agentHasLogin(cfg Config) func() bool {
	return func() bool {
		dir, err := agentClaudeHomeDir(cfg)
		return err == nil && claudehome.NewHome(dir).HasLogin()
	}
}

// legacyClaudeDir is the person's own ~/.claude, searched read-only for
// transcripts older than the agent Claude home.
func legacyClaudeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func newClaudeTranscripts(h claudehome.Home) claudehome.Transcripts {
	return claudehome.Transcripts{Dir: h.Path(), Legacy: legacyClaudeDir()}
}

func newClaudeTranscriptReader(h claudehome.Home) *claudehome.TranscriptReader {
	return &claudehome.TranscriptReader{Dir: h.Path(), Legacy: legacyClaudeDir()}
}

func newHub() *orchestration.Hub { return orchestration.NewHub(256) }

// newOrchestrationService hands the orchestration the catalog contexts it
// reads to set sessions up.
// newArchitectureRules checks sessions' actions against their repository's
// architecture rules, reading each repository's current saved record and
// announcing every failed rule on the orchestration's project feed.
func newArchitectureRules(cat catalog.Catalog, sessions ports.SessionRepository, events ports.ArchitectureRuleEventStore, orch *orchestration.Service) *archrules.Service {
	svc := archrules.NewService(archrules.Deps{
		Sessions: sessions, Agents: cat.Agents, Records: cat.RepositoryArchitectures, Events: events, Feed: orch, EnvFiles: cat.Projects,
	})
	svc.Subscribe(cat.Bus)
	return svc
}

func newOrchestrationService(
	cfg Config,
	commitGuard ports.CommitGuard,
	manager llmkit.Manager,
	broker *approval.Broker,
	hub *orchestration.Hub,
	sessions ports.SessionRepository,
	cat catalog.Catalog,
	tickets ports.TicketRepository,
	ws *workspaces.Service,
	transcripts ports.ClaudeTranscripts,
	transcriptReader ports.ClaudeTranscriptReader,
	terminals ports.TerminalHost,
	sandboxDriver ports.SandboxDriver,
	violations ports.SandboxViolationSource,
	violationStore ports.SandboxViolationStore,
	agentHome claudehome.Home,
	claudeBin ports.ManagedClaudeBin,
	branchNames *workspaces.BranchNames,
) *orchestration.Service {
	svc := orchestration.NewService(manager, broker, hub, sessions, orchestration.Catalog{
		Projects:     cat.Projects,
		Repositories: cat.Projects,
		Agents:       cat.Agents,
		MCPServers:   cat.Capabilities,
		Zones:        cat.Architecture,
		Contexts:     cat.Architecture,
	}, tickets, ws)
	svc.UseBranchNames(branchNames)
	if cat.RepositoryArchitectures != nil {
		// Saves, deletes and exports reach open pages on the project feed.
		cat.RepositoryArchitectures.UseFeed(svc)
		// Briefs name the record and the layers the session's agent may change.
		svc.Architectures = cat.RepositoryArchitectures
		// A commit in a session worktree is checked against the layers.
		svc.CommitGuard = commitGuard
	}
	base := cfg.loopbackBaseURL()
	// The route belongs to an inbound adapter, so the application layer is
	// handed the URL rather than importing the adapter to build it.
	svc.TaskServerURL = func(sessionID string) string {
		return base + mcpsession.PathPrefix + sessionID
	}
	svc.SessionHookURL = func(sessionID string) string {
		return base + httpapi.InteractiveSessionHookPath + "?session_id=" + url.QueryEscape(sessionID)
	}
	svc.SessionShellURL = func(sessionID string) string {
		return base + httpapi.SessionShellPath(sessionID)
	}
	svc.SessionArchitectureURL = func(sessionID string) string {
		return base + httpapi.SessionArchitecturePath(sessionID)
	}
	svc.ShimBinary = cfg.ShimBinary
	svc.DeveloperBin = developerdir.Bin
	svc.TokenSavers = cat.Capabilities
	svc.Settings = cat.Settings
	svc.Transcripts = transcripts
	svc.TranscriptReader = transcriptReader
	svc.Terminals = terminals
	svc.Events = cat.Bus
	toolsDir, _ := dataDir(cfg, "tools")
	userHome, _ := os.UserHomeDir()
	allow := sandboxAllowlist(allowSources{
		Home: userHome, ToolsDir: toolsDir, ClaudeDirs: runtime.ClaudeInstallDirs(claudeBin.Bin()), Getenv: os.Getenv,
	})
	// A platform without a driver leaves Driver nil: with the sandbox on,
	// a session start then refuses (ADR 0017).
	svc.Sandbox = &orchestration.Sandbox{
		Driver:  sandboxDriver,
		Enabled: orchestration.SandboxSwitch(cat.Settings),
		Home:    agentHome.Path(),
		// Their git config is read-only to a session; not their ssh folder.
		UserHome:   userHome,
		ConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		Bin:        claudeBin.Bin(),
		Allow:      func(string) []ports.SandboxPath { return allow },
		// What no session grants is hidden: the workspaces root and, for
		// one-shots, every checkout.
		WorkspacesRoot: ws.Root,
		Checkouts:      func() []string { return allCheckouts(cat.Projects) },
		// A project's agents read every repository of it unless the person
		// turned that off on the project's settings page; a one-shot has no
		// project and keeps the default.
		ReadAllRepos: func(projectID string) bool { return projectReadsAll(cat.Projects, projectID) },
		// Each session start gets a temp dir of its own, never the system's,
		// which every session (and each one's virtual shell) shares: this is
		// its name prefix, the start adds a random suffix. Short, so a unix
		// socket path under it stays within the platform's limit.
		TempDir: func(id string) string { return sessionTempPrefix(os.TempDir(), id) },

		Violations:     violations,
		ViolationStore: violationStore,
	}
	return svc
}

// projectReadsAll is the project's setting "Agents can read every repository
// of this project". An unknown project, a failed read and a one-shot (no
// project) keep the setting's default, on.
func projectReadsAll(projects ports.ProjectReader, projectID string) bool {
	if projectID == "" {
		return true
	}
	p, err := projects.GetProject(context.Background(), projectID)
	if err != nil || p == nil {
		return true
	}
	return p.AgentsReadAllRepos
}

// allCheckouts lists the checkout of every repository of every project.
func allCheckouts(projects interface {
	ports.ProjectReader
	ports.RepositoryLister
}) []string {
	ctx := context.Background()
	all, err := projects.ListProjects(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range all {
		repos, err := projects.ListRepositories(ctx, p.ID)
		if err != nil {
			continue
		}
		for _, r := range repos {
			out = append(out, r.RootDir)
		}
	}
	return out
}

// TextProcessingModule binds the one-shot Claude CLI adapters to the text
// processing ports. Each port gets its own adapter so a consumer depends on the
// single capability it uses rather than on "the text processor".
var TextProcessingModule = fx.Module("textprocessing",
	fx.Provide(fx.Private, newOneShotter, newStreamOneShotter),
	fx.Provide(
		newWritingHelper,
		asPort(newSummarizer, new(ports.Summarizer)),
		asPort(newTextGenerator, new(ports.TextGenerator)),
		asPort(newTranslator, new(ports.Translator)),
	),
)

// oneShotParts are what the one-shots run on, each when the graph has it:
// what is absent leaves the one-shot as it was.
type oneShotParts struct {
	fx.In

	Ready      optionalAgentReadiness
	Orch       *orchestration.Service   `optional:"true"`
	Credential *agentcredential.Service `optional:"true"`
	Bin        ports.ManagedClaudeBin   `optional:"true"`
	Home       claudehome.Home          `optional:"true"`
	KC         *keychainshim.Dir        `optional:"true"`
}

// newOneShotter is the claude driver the one-shots run on: llmkit's one-shot
// mode, routed through the LLM proxy like every other claude mode. One-shots
// need nothing else of a manager, so the module keeps its own. Like a session
// it runs the managed claude (or the -claude.bin override) isolated in the
// agent Claude home, with the agent credential, under the sandbox when it is
// on (ADR 0017, 0018), after the readiness gate.
func newOneShotter(cfg Config, p oneShotParts) claudetext.OneShotter {
	bin := oneShotBin(cfg, p)
	one := claudetext.OneShotter(oneShotClaude(bin, p.Home.Path(), cfg.llmProxyURL()))
	if prep := oneShotPreparer(bin, p); prep != nil {
		one = claudetext.Confined(one, prep)
	}
	return claudetext.Gated(one, p.Ready.check())
}

// newStreamOneShotter is newOneShotter's streaming twin, for the writing
// helper (ADR 0026): the same claude, home, credential, sandbox and gate.
func newStreamOneShotter(cfg Config, p oneShotParts) claudetext.StreamOneShotter {
	bin := oneShotBin(cfg, p)
	one := claudetext.StreamOneShotter(oneShotClaude(bin, p.Home.Path(), cfg.llmProxyURL()))
	if prep := oneShotPreparer(bin, p); prep != nil {
		one = claudetext.ConfinedStream(one, prep)
	}
	return claudetext.GatedStream(one, p.Ready.check())
}

// oneShotBin is the claude binary one-shots run.
func oneShotBin(cfg Config, p oneShotParts) string {
	if p.Bin != nil {
		return p.Bin.Bin()
	}
	return cfg.ClaudeBin
}

// oneShotPreparer prepares each one-shot for the agent isolation, nil when
// the graph has no sandbox or credential.
func oneShotPreparer(bin string, p oneShotParts) func() ([]string, llmkit.WrapFunc, error) {
	if p.Orch == nil || p.Credential == nil {
		return nil
	}
	return oneShotPrep(p.Orch.Sandbox, oneShotEnv(p.Credential.Env, p.Home.HasLogin, p.KC.Path()), bin, p.KC.Path())
}

// newWritingHelper is the writing helper over the streaming one-shot, the
// global model setting and the project catalog. Its steps are behind the gate
// every one-shot is behind (the streaming one-shotter's); what a page shows is
// the gate's state, read without starting an install.
func newWritingHelper(cat catalog.Catalog, one claudetext.StreamOneShotter, p oneShotParts) *writinghelper.Service {
	return writinghelper.New(streamer{one}, cat.Settings, cat.Projects, p.Ready.peek())
}

// streamer adapts the streaming one-shot to the writing helper's port.
type streamer struct{ one claudetext.StreamOneShotter }

func (s streamer) Stream(ctx context.Context, c writinghelper.StreamCall, onText func(string)) (string, error) {
	res, err := s.one.OneShotStream(ctx, claude.OneShotRequest{System: c.System, Input: c.Input, Model: c.Model, Attribution: c.Attribution}, onText)
	return res.Text, err
}

// oneShotClaude is the manager one-shots run on: isolated in the agent
// Claude home when there is one.
func oneShotClaude(bin, home, proxyURL string) *claude.Manager {
	return claude.New(llmkit.Options{Bin: bin, ConfigDir: home, Isolated: home != "", ProxyURL: proxyURL})
}

// oneShotPrep prepares each one-shot: the env entries of the agent credential
// and, with the sandbox on, the wrap confining it. The sandbox's policy for a
// one-shot names no repository; claude's install dirs are the one thing
// beyond its temp dir and the agent home it may read.
// readable are further directories it may read, the security shim's.
func oneShotPrep(sb *orchestration.Sandbox, credential func() []string, bin string, readable ...string) func() ([]string, llmkit.WrapFunc, error) {
	allow := sandboxAllowlist(allowSources{ClaudeDirs: append(runtime.ClaudeInstallDirs(bin), readable...)})
	return func() ([]string, llmkit.WrapFunc, error) {
		wrap, err := sb.OneShotWrap(allow)
		if err != nil {
			return nil, nil, err
		}
		return credential(), wrap, nil
	}
}

// oneShotEnv is the env entries of a one-shot: the pasted credential unless
// the agent home holds a login, and the security shim's dir on PATH.
func oneShotEnv(pasted func() []string, login func() bool, shimDir string) func() []string {
	return func() []string {
		var env []string
		if !login() {
			env = pasted()
		}
		return append(env, keychainpath.Env(os.Environ(), shimDir)...)
	}
}

func newSummarizer(cfg Config, c claudetext.OneShotter) *claudetext.Summarizer {
	return claudetext.NewSummarizer(c, cfg.ClaudeBin, cfg.ClaudeTextModel)
}

func newTextGenerator(cfg Config, c claudetext.OneShotter) *claudetext.TextGenerator {
	return claudetext.NewTextGenerator(c, cfg.ClaudeBin, cfg.ClaudeTextModel)
}

func newTranslator(cfg Config, c claudetext.OneShotter) *claudetext.Translator {
	return claudetext.NewTranslator(c, cfg.ClaudeBin, cfg.ClaudeTextModel)
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
		asPort(newWorktreeManager, new(ports.WorktreeManager)),
		asPort(newGitHistory, new(ports.GitHistory)),
		asPort(newBranchPublisher, new(ports.BranchPublisher)),
		asPort(newGitMerger, new(ports.GitMerger)),
		asPort(newGitBranchNames, new(ports.GitBranchNames)),
		asPort(githubapi.New, new(ports.CodeHost)),
		asPort(newGitCode, new(ports.GitCode)),
		asPort(filesystem.NewWorktreeFiles, new(ports.WorktreeFiles)),
		newWorkspacesService,
		newPullRequestsService,
		newMergesService,
		newBranchNamesService,
		newCodeBrowser,
		newMergeStates,
	),
	fx.Invoke(registerCodeWatcher),
)

// newCodeBrowser serves the code views: repositories and worktrees read
// through git and the filesystem, zoned by the architecture context's read
// port.
func newCodeBrowser(ws ports.WorkspaceRepository, repos ports.RepositoryRepository, git ports.GitCode, files ports.WorktreeFiles, cat catalog.Catalog) ports.CodeBrowser {
	return workspaces.NewCode(workspaces.CodeDeps{
		Workspaces: ws, Repositories: repos, Git: git, Files: files, Architecture: cat.Architecture,
	})
}

// registerCodeWatcher announces code_changed on the orchestration's project
// feed while a workspace's session runs and someone follows its project's
// feed, polling from start to stop. The merge states hear each change too.
func registerCodeWatcher(lc fx.Lifecycle, sessions ports.SessionRepository, ws ports.WorkspaceRepository, git ports.GitCode, orch *orchestration.Service, merges *workspaces.MergeStates, cat catalog.Catalog) {
	w := workspaces.NewCodeWatcher(workspaces.CodeWatcherDeps{Sessions: sessions, Workspaces: ws, Git: git, Feed: changeSinks{orch, merges}})
	w.Subscribe(cat.Bus)
	orch.ObserveFeedFollowers(w)
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			w.Start()
			return nil
		},
		OnStop: func(context.Context) error {
			w.Stop()
			return nil
		},
	})
}

// newMergeStates reads how much of each review or done task's code landed
// in its repository's default branch, announcing a task through planning when
// its merge state changes, and stops its background refreshes on shutdown.
func newMergeStates(lc fx.Lifecycle, cfg Config, sessions ports.SessionRepository, ws ports.WorkspaceRepository, repos ports.RepositoryRepository, tickets ports.TicketRepository, plan *planning.Service) *workspaces.MergeStates {
	svc := workspaces.NewMergeStates(workspaces.MergeStateDeps{
		Sessions: sessions, Workspaces: ws, Repositories: repos, Git: gitcli.NewBranchMerges(cfg.gitOptions()...),
		Tickets: tickets, Announcer: plan,
	})
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		svc.Close()
		return nil
	}})
	return svc
}

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

// WithCodeHost binds host in place of the GitHub adapter, so a test of the
// wired server opens pull requests without reaching GitHub. See package
// githubapitest for driving its fake.
func WithCodeHost(host ports.CodeHost) fx.Option {
	return fx.Decorate(func(ports.CodeHost) ports.CodeHost { return host })
}

// newPullRequestsService opens workspaces' branches as pull requests with
// the stored git credentials only. It announces on the orchestration's
// project feed and to the merge states, reads the task of a workspace's
// session from planning for the suggested title and body, drops a deleted project's records, and stops its
// background host lookups on shutdown.
func newPullRequestsService(
	lc fx.Lifecycle,
	ws ports.WorkspaceRepository,
	repos ports.RepositoryRepository,
	history ports.GitHistory,
	git ports.BranchPublisher,
	host ports.CodeHost,
	store ports.PullRequestRepository,
	sessions ports.SessionRepository,
	plan *planning.Service,
	orch *orchestration.Service,
	merges *workspaces.MergeStates,
	cat catalog.Catalog,
	creds *gitcredentials.Service,
) *workspaces.PullRequests {
	svc := workspaces.NewPullRequests(workspaces.PullRequestDeps{
		Workspaces: ws, Repositories: repos, History: history, Git: git, Host: host, Store: store,
		Sessions: sessions, Tickets: plan, Feed: changeSinks{orch, merges}, Auth: creds,
	})
	svc.Subscribe(cat.Bus)
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		svc.Close()
		return nil
	}})
	return svc
}

// newMergesService merges a session's branch into its repository's default
// branch in the local checkout, and announces the result on the
// orchestration's project feed.
func newMergesService(
	sessions ports.SessionRepository,
	ws ports.WorkspaceRepository,
	repos ports.RepositoryRepository,
	git ports.GitMerger,
	orch *orchestration.Service,
) *workspaces.Merges {
	return workspaces.NewMerges(workspaces.MergeDeps{Sessions: sessions, Workspaces: ws, Repositories: repos, Git: git, Announce: orch})
}

// newBranchNamesService checks the names a person types for a session's
// branch, against git's rules and the repository's local branches.
func newBranchNamesService(repos ports.RepositoryRepository, tickets ports.TicketRepository, git ports.GitBranchNames) *workspaces.BranchNames {
	return workspaces.NewBranchNames(repos, tickets, git)
}

// shortID is the part of a session id that names its temp dir: short enough
// to keep unix socket paths under the 104 byte limit, long enough to be unique.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// sessionTempPrefix is the name prefix of a session's temp dir under base:
// hs-<8 id characters>; each start adds "-" and nine random digits. It is as
// short as it is because Playwright's browser MCP server keeps a unix socket
// at <temp dir>/browser/<14 characters>.sock and refuses a path over 103
// bytes; a macOS system temp dir is already 50 bytes, so the whole session
// temp dir may take 25 more. The name stays short, never "harness-session-".
func sessionTempPrefix(base, id string) string {
	return filepath.Join(base, "hs-"+id[:min(len(id), 8)])
}
