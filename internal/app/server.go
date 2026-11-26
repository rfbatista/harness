package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/application/sandboxsettings"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/llm"
	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/in/ui"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/adapter/out/agentcatalog"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentboard"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/archassistant"
	"operators-mcp/internal/application/archrules"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/branchgate"
	"operators-mcp/internal/application/branchwatch"
	"operators-mcp/internal/application/contextmap"
	"operators-mcp/internal/application/execution"
	"operators-mcp/internal/application/gateruns"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/personproof"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/productowner"
	"operators-mcp/internal/application/repoagents"
	"operators-mcp/internal/application/servicerequests"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/usage"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/application/writinghelper"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rfbatista/llmkit/approval"
	"github.com/rfbatista/llmkit/claude/mcpapprove"
	llminterview "github.com/rfbatista/llmkit/interview"
)

// ServerModule starts the inbound servers: the HTTP server (UI + JSON API) and
// the MCP streamable HTTP server the IDE connects to. Both are registered as
// fx.Invoke so they always boot, and their listen/shutdown is driven by fx
// lifecycle hooks.
var ServerModule = fx.Module("server",
	fx.Invoke(registerHTTPServer),
	fx.Invoke(registerMCPServer),
)

// registerHTTPServer mounts the UI and JSON API on a mux and serves it,
// shutting down gracefully when fx stops.
func registerHTTPServer(lc fx.Lifecycle, cfg Config, cat catalog.Catalog, appRunner *apps.Service, svcs *services.Service, svcRequests *servicerequests.Service, ts *tooling.Service, exec *execution.Service, orch *orchestration.Service, plan *planning.Service, ws *workspaces.Service, broker *approval.Broker, sessions ports.SessionRepository, art *artifacts.Service, channel *taskchannel.Service, prs *workspaces.PullRequests, merges *workspaces.Merges, archivedWorktrees *workspaces.ArchivedWorktrees, branchNames *workspaces.BranchNames, code ports.CodeBrowser, worktreeGit ports.WorktreeGit, inbox optionalNotifications, creds optionalGitCredentials, shells *vfs.Service, fsys *vfs.FileSystem, usageSvc *usage.Service, indexes optionalRepositoryIndexes, sandboxStore *sandboxsettings.Store, agentSystem *agentsystem.Service, contextMap *contextmap.Service, managedClaude *managedclaude.Service, claudeBin ports.ManagedClaudeBin, agentCred optionalAgentCredential, helper *writinghelper.Service, assistant *archassistant.Service, rules *archrules.Service, gate *branchgate.Service, watch *branchwatch.Service, agentCat *agentcatalog.Catalog, persons *vshell.PersonTerminals, repoAgents *repoagents.Service, board optionalAgentBoard, asked optionalInterviews, owner optionalProductOwner, gateRuns *gateruns.Service, declarer *gateruns.Declarer, proof *personproof.Service) error {
	uiHandler, err := ui.SPAHandler(ui.Dist)
	if err != nil {
		return err
	}
	assets, err := web.NewAssets()
	if err != nil {
		return err
	}
	if !assets.Built() {
		slog.Warn("the web client is not built; pages render without styles or scripts. Run: make web")
	}

	// An interface holding a nil pointer is not nil: hand the inbox on only
	// when NotificationsModule built one.
	var (
		notes      ports.Notifications
		noteReader ports.NotificationReader
		notifier   ports.Notifier
		resumeAll  ports.InterruptedResumer
	)
	if inbox.Service != nil {
		notes, noteReader, notifier, resumeAll = inbox.Service, inbox.Service, inbox.Service, inbox.Service
	}

	// Same for the Agent board.
	var agentBoard ports.AgentBoard
	if board.Service != nil {
		agentBoard = board.Service
	}

	// Same for the product owner.
	var productOwner ports.ProductOwner
	var productOwnerMemory ports.ProductOwnerMemoryTools
	if owner.Service != nil {
		productOwner = owner.Service
		productOwnerMemory = owner.Service
	}

	// Same for the interviews.
	var interviewsPort ports.Interviews
	if asked.Service != nil {
		interviewsPort = asked.Service
	}

	// Discovery offers the platform's sandbox and what the person added to it.
	if cat.RepositoryArchitectures != nil {
		cat.RepositoryArchitectures.UseSandbox(sandboxDriver(orch), sandboxStore)
	}

	// The architecture assistant: an interface holding a nil pointer is not
	// nil, so the pages and the API get it only when the graph has one. Its
	// conversations are swept a day after their last turn.
	var architectureAssistant ports.ArchitectureAssistant
	if assistant != nil {
		architectureAssistant = assistant
		sweepCtx, stopSweeping := context.WithCancel(context.Background())
		lc.Append(fx.Hook{
			OnStart: func(context.Context) error {
				go assistant.RunSweeper(sweepCtx, time.Hour)
				return nil
			},
			OnStop: func(context.Context) error { stopSweeping(); return nil },
		})
	}

	mux := http.NewServeMux()
	// The top bar's model is built by the web adapter from the pages' own
	// dependencies, which are assembled below, after the API.
	topBars := &lateTopBars{}
	apiRouter := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		TopBars:         topBars,
		GateRuns:        gateRuns,
		Pairing:         proof,
		Projects:        cat.Projects,
		ProjectFeed:     cat.Projects,
		Discovery:       cat.Projects,
		Env:             cat.Projects,
		RunCommands:     cat.Projects,
		Apps:            appRunner,
		Services:        svcs,
		ServiceRequests: svcRequests,
		Architecture:    cat.Architecture,
		Agents:          cat.Agents,
		Capabilities:    cat.Capabilities,
		TokenSavers:     cat.Capabilities,
		MemoryBudgets:   cat.Capabilities,
		Settings:        cat.Settings,
		Tools:           ts,
		Tasks:           exec,
		Sessions:        orch,
		// The person's own terminals on a session's files.
		PersonTerminals: persons,
		Planning:        plan,
		Workspaces:      ws,
		Artifacts:       art,
		TaskChannel:     channel,
		// What a person adds to a task: files, images, audio, links, comments.
		TaskResources:      plan,
		TaskComments:       plan,
		TaskArchive:        plan,
		PullRequests:       prs,
		PullRequestBases:   prs,
		Merges:             merges,
		ArchivedWorktrees:  archivedWorktrees,
		BranchNames:        branchNames,
		Notifications:      notes,
		InterruptedResumer: resumeAll,
		AgentBoard:         agentBoard,
		Interviews:         interviewsPort,
		Usage:              usageSvc,
		LiveCalls:          usageSvc,
		Code:               code,
		WorktreeGit:        worktreeGit,
		// A session's virtual file system, for the Files view.
		SessionFiles: fsys,
		// The git credentials every remote operation authenticates with.
		GitCredentials: creds.port(),
		// What sessions write in their virtual shells.
		Shells: shells,
		// Token savers, installed from the releases the harness pins.
		TokenSaverInstalls: cat.Capabilities,
		// Each repository's codegraph index, built in its checkout.
		RepositoryIndexes: indexes.port(),
		// The Agent isolation page's switch and extra paths.
		SandboxSettings: sandboxStore,
		SandboxDriver:   sandboxDriver(orch),
		// The agent system settings: project defaults and repository overrides.
		AgentSystem: agentSystem,
		// The managed claude agents run, and which binary they resolve to.
		ManagedClaude:    managedClaude,
		ManagedClaudeBin: claudeBin,
		// The credential agents sign in with: status only.
		AgentCredential: agentCred.port(),
		// The credential one project's agents use instead.
		ProjectAgentCredentials: agentCred.projectPort(),
		// The Sign in with Claude flow that stores it.
		AgentOAuth: agentCred.oauth(),
		AgentAuth:  agentCred.auth(),
		// Each project's own Sign in with Claude.
		ProjectAgentOAuth: agentCred.projectOAuth(),
		// The magic pen's steps, streamed.
		WritingHelper: helper,
		// The architecture assistant's turns, streamed.
		ArchitectureAssistant: architectureAssistant,
		// One structured record per repository: layers, access, rules.
		RepositoryArchitectures: cat.RepositoryArchitectures,
		// What agents proposed for a repository's instruction files.
		InstructionProposals:         cat.InstructionProposals,
		TaskMemories:                 cat.TaskMemories,
		ContextMaps:                  contextMap,
		InstructionProposalDecisions: cat.InstructionProposals,
		// One structured record per project: tokens, principles, components.
		DesignSystems: cat.DesignSystems,
		// What the architecture hooks, shims and commit check ask.
		ArchitectureRules: rules,
		// What a session's git shim asks before a branch action.
		BranchGate: gate,
		// The projects' product owners: settings and threads.
		ProductOwner: productOwner,
		// The Branches lists, read from stored state.
		SessionBranches: watch,
		// The agent catalog folder: agents, bundles, skills, MCP servers.
		Catalog: agentCat,
		// Which agents each repository assigns, and its repository prompts.
		RepositoryAgents: repoAgents,
	}), httpapi.WithToken(cfg.APIToken), httpapi.WithExposedListener(exposed(cfg.HTTPAddr)),
		// The routes that are the person's alone need the credential they got by pairing (ADR 0016).
		httpapi.WithPersonProof(proof, personRefusals{events: cat.Bus}))
	// Requests slower than the threshold are logged with their query count.
	// An approval waits on a person, so it is never a slow request.
	slow := func(h http.Handler) http.Handler {
		return httpapi.SlowRequestLog(h, httpapi.WithSlowRequestThreshold(cfg.SlowRequestThreshold))
	}
	mux.Handle("/api/", slow(apiRouter))
	mux.Handle(mcpapprove.PathPrefix, mcpapprove.Handler(broker))
	if asked.Service != nil {
		mux.Handle(llminterview.PathPrefix, interviewHandler(asked.Service))
	}
	mux.Handle(mcpsession.PathPrefix, slow(sessionTaskHandler(sessionTaskTools(cat, orch, plan, sessions, art, channel, notifier, merges, tooling.WithAgentPlacements(repoAgents), tooling.WithModeAgents(cat.Settings), tooling.WithBranchPush(gate), tooling.WithDeclarations(declarer)), plan, sessions, productOwnerMemory, append(ownerSources(owner.Service), tooling.WithConversationSearch(productOwner))...)))
	mux.Handle(mcpsession.ServicesPathPrefix, slow(mcpsession.ServicesHandler(tooling.SessionServicesTools(sessions, cat.Agents, svcs, cat.Projects, tooling.WithServiceRequests(svcRequests)))))
	// The web client owns / and its pages; anything else reaches the legacy
	// designer SPA until it is retired.
	webDeps := web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects, RunCommands: cat.Projects, Documents: plan, History: ws, Code: code, Workspaces: ws, Artifacts: art, ProjectSummaries: cat.Projects, PullRequests: prs, WorktreeGit: worktreeGit,
		DesignSystems: designSystemReader(cat), Resources: plan, Comments: plan, Notifications: noteReader, BranchRequests: gate, SessionBranches: watch, GitCredentials: creds.port(), Usage: usageSvc, Services: svcs, ServiceCounter: svcs, TokenSavers: cat.Capabilities, MemoryBudgets: cat.Capabilities,
		TokenSaverInstalls: cat.Capabilities, RepositoryIndexes: indexes.port(), Settings: cat.Settings, DataPaths: dataPaths(cfg), SandboxSettings: sandboxStore, SandboxDriver: sandboxDriver(orch), SandboxBuiltin: sandboxBuiltin(orch),
		ManagedClaude: managedClaude, ManagedClaudeBin: claudeBin, AgentCredential: agentCred.port(), ProjectAgentCredentials: agentCred.projectPort(),
		AgentOAuth: agentCred.oauth(), AgentAuth: agentCred.auth(), ProjectAgentOAuth: agentCred.projectOAuth(), AgentHomeExposed: agentHomeExposed(cfg), AgentLogin: agentHasLogin(cfg), ArchivedTasks: plan, ArchivedList: plan,
		// Whether the pens work, read from the gate without starting anything.
		WritingHelper: helperState(helper), ArchitectureAssistant: architectureAssistant, OpenInstructionProposals: openProposals(cat.InstructionProposals), AgentCatalog: CatalogSeed(agentCat, repoAgents), AgentBoard: agentBoard, Interviews: interviewsPort, ProductOwnerThreads: productOwner}
	topBars.set(web.NewTopBars(webDeps))
	mux.Handle("/", slow(web.NewHandler(webDeps, assets, uiHandler)))

	// The LLM proxy every claude process calls through (ADR 0006): loopback
	// only, outside the API token and CORS, so its responses come back as
	// the upstream sent them and no web page is invited to use it.
	llmProxy, err := llm.Handler(llm.Config{Sink: usageSvc, Capture: usageSvc})
	if err != nil {
		return err
	}
	root := http.NewServeMux()
	root.Handle(llm.PathPrefix, llmProxy)
	root.Handle("/", httpapi.CORSMiddleware(mux))

	srv := newHTTPServer(cfg.HTTPAddr, withProfiler(!cfg.ProfilerOff, root))
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			go func() {
				if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
					slog.Error("http server stopped", "err", err)
				}
			}()
			announcePairing(proof, ln.Addr().String())
			slog.Info("HTTP server listening", "addr", srv.Addr, "ui", "/", "api", "/api", "token", cfg.APIToken != "")
			if exposed(srv.Addr) && cfg.APIToken == "" {
				slog.Warn("THE API IS OPEN TO THE NETWORK: "+srv.Addr+" listens beyond this machine and no token is set. "+
					"Anyone who can reach it can read and change projects, start agents, and type into their terminals. "+
					"Set CODING_POOL_TOKEN (or --api.token), or listen on 127.0.0.1.", "addr", srv.Addr)
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			return srv.Shutdown(shutdownCtx)
		},
	})
	return nil
}

// personRefusals turns a refused call to a person-only route into an event the
// notifications context reports (ADR 0016).
type personRefusals struct{ events ports.EventPublisher }

func (p personRefusals) Refused(ctx context.Context, r ports.PersonRefusal) {
	if p.events == nil {
		return
	}
	_ = p.events.Publish(ctx, domain.PersonOnlyRefused{Method: r.Method, Route: r.Route, ProjectID: r.ProjectID, At: time.Now()})
}

// announcePairing prints the link that pairs the person's browser when nobody is
// paired yet (ADR 0016). It goes to the terminal the harness runs in and
// nowhere else: not the log, which may be a file, and not any file, the data
// directory, or a session's environment.
func announcePairing(proof *personproof.Service, addr string) {
	code, ok := proof.FirstCode(context.Background())
	if !ok {
		return
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || host == "::" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	fmt.Fprintf(os.Stdout, "\nPair this browser with the harness, once, so it can answer requests:\n  http://%s:%s%s?code=%s\n\n", host, port, httpapi.PairPath, code)
}

// optionalAgentBoard is the Agent board when AgentBoardModule is in the graph;
// graphs built without it (some tests) serve none.
type optionalAgentBoard struct {
	fx.In

	Service *agentboard.Service `optional:"true"`
}

// optionalProductOwner is the product owner when ProductOwnerModule is in the
// graph; graphs built without it (some tests) serve none.
type optionalProductOwner struct {
	fx.In

	Service *productowner.Service `optional:"true"`
}

// optionalNotifications is the inbox when NotificationsModule is in the
// graph; graphs built without it (some tests) serve no notifications.
type optionalNotifications struct {
	fx.In

	Service *notifications.Service `optional:"true"`
}

// sessionTaskTools are the tools each session reaches on its own task
// endpoint, /mcp/task/{session}.
func sessionTaskTools(cat catalog.Catalog, orch *orchestration.Service, plan *planning.Service, sessions ports.SessionRepository, art *artifacts.Service, channel *taskchannel.Service, notifier ports.Notifier, merges ports.BranchMerger, extra ...tooling.Option) []domain.Tool {
	opts := append([]tooling.Option{}, extra...)
	if cat.DesignSystems != nil {
		opts = append(opts, tooling.WithDesignProposals(cat.DesignSystems))
	}
	if cat.InstructionProposals != nil {
		opts = append(opts, tooling.WithInstructionProposals(cat.InstructionProposals))
	}
	return tooling.SessionTaskTools(plan, sessions, cat.Agents, cat.Architecture,
		tooling.PeerStarter{Sessions: orch, Stopper: orch, Repositories: cat.Projects, Merger: merges},
		tooling.ArtifactTooling{Publisher: art, Attachments: art, ViewURL: httpapi.ArtifactViewPath}, channel,
		tooling.NotificationTooling{Notifier: notifier}, opts...)
}

// registerMCPServer builds the MCP server, registers all tools and the designer
// resource, and serves it over streamable HTTP for IDE clients.
func registerMCPServer(lc fx.Lifecycle, cfg Config, ts *tooling.Service) {
	s := mcpserver.NewMCPServer("operators-mcp", "0.0.1", mcpserver.WithToolCapabilities(true))
	mcp.RegisterTools(s, ts)

	designerResource := mcplib.NewResource(ui.DesignerURI, "Designer",
		mcplib.WithResourceDescription("Architecture Designer UI"),
		mcplib.WithMIMEType("text/html"),
	)
	s.AddResource(designerResource, designerResourceHandler(cfg.DevMode))

	stream := mcpserver.NewStreamableHTTPServer(s)
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := stream.Start(cfg.MCPAddr); err != nil && err != http.ErrServerClosed {
					slog.Error("mcp server stopped", "err", err)
				}
			}()
			slog.Info("MCP server listening", "addr", cfg.MCPAddr, "path", "/mcp")
			if exposed(cfg.MCPAddr) {
				slog.Warn("THE MCP SERVER IS OPEN TO THE NETWORK: "+cfg.MCPAddr+" listens beyond this machine and takes no token. "+
					"Listen on 127.0.0.1 unless every client that can reach it is trusted.", "addr", cfg.MCPAddr)
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			return stream.Shutdown(shutdownCtx)
		},
	})
}

// designerResourceHandler serves the architecture designer UI as an MCP
// resource, either from the embedded build or proxied to the Vite dev server.
func designerResourceHandler(devMode bool) func(context.Context, mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
	var embedFS fs.FS
	if !devMode {
		embedFS = ui.Dist
	}
	devURL := ui.DefaultDevServerURL
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		if req.Params.URI != ui.DesignerURI {
			return nil, nil
		}
		html, mime, err := ui.DesignerContent(ctx, devMode, embedFS, devURL)
		if err != nil {
			return nil, err
		}
		return []mcplib.ResourceContents{
			mcplib.TextResourceContents{
				URI:      ui.DesignerURI,
				MIMEType: mime,
				Text:     html,
			},
		}, nil
	}
}

// designSystemReader is the design system context as the web pages read it;
// nil, not a typed nil, when it is not built.
func designSystemReader(cat catalog.Catalog) ports.DesignSystemPages {
	if cat.DesignSystems == nil {
		return nil
	}
	return cat.DesignSystems
}

// exposed reports whether a listen address reaches beyond this machine: no
// host (every interface), or a host that is not loopback.
func exposed(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return true
	}
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// dataPaths are where the server keeps its data, for the Server page.
func dataPaths(cfg Config) *serversettings.Paths {
	p := &serversettings.Paths{Source: cfg.DataDirSource, Database: cfg.DBPath}
	if cfg.inMemory() {
		p.Database = ":memory:"
	} else if db, err := filepath.Abs(cfg.DBPath); err == nil {
		p.Database = db
	}
	root, err := cfg.dataRoot()
	if err != nil {
		slog.Warn("server page: data dir not resolved", "err", err)
	}
	p.DataDir = root
	if root != "" && p.Source == "" {
		p.Source = "the database path"
	}
	return p
}

// lateTopBars is the API's top bar reader before the web adapter's
// dependencies exist: set once, while the server is assembled.
type lateTopBars struct{ next ports.TopBars }

func (l *lateTopBars) set(t ports.TopBars) { l.next = t }

func (l *lateTopBars) TopBar(ctx context.Context, path string) (*ports.TopBar, error) {
	if l.next == nil {
		return nil, errors.New("the top bar is not assembled yet")
	}
	return l.next.TopBar(ctx, path)
}
