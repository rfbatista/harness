package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/application/sandboxsettings"
	"path/filepath"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/llm"
	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/in/ui"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/execution"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/usage"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rfbatista/llmkit/approval"
	"github.com/rfbatista/llmkit/claude/mcpapprove"
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
func registerHTTPServer(lc fx.Lifecycle, cfg Config, cat catalog.Catalog, appRunner *apps.Service, ts *tooling.Service, exec *execution.Service, orch *orchestration.Service, plan *planning.Service, ws *workspaces.Service, broker *approval.Broker, sessions ports.SessionRepository, art *artifacts.Service, channel *taskchannel.Service, prs *workspaces.PullRequests, merges *workspaces.Merges, code ports.CodeBrowser, inbox optionalNotifications, creds optionalGitCredentials, shells *vshell.Service, usageSvc *usage.Service, indexes optionalRepositoryIndexes, sandboxStore *sandboxsettings.Store, managedClaude *managedclaude.Service, claudeBin ports.ManagedClaudeBin, agentCred optionalAgentCredential) error {
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
	)
	if inbox.Service != nil {
		notes, noteReader, notifier = inbox.Service, inbox.Service, inbox.Service
	}

	mux := http.NewServeMux()
	apiRouter := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects:     cat.Projects,
		ProjectFeed:  cat.Projects,
		Discovery:    cat.Projects,
		Env:          cat.Projects,
		RunCommands:  cat.Projects,
		Apps:         appRunner,
		Architecture: cat.Architecture,
		Agents:       cat.Agents,
		Capabilities: cat.Capabilities,
		TokenSavers:  cat.Capabilities,
		Settings:     cat.Settings,
		Tools:        ts,
		Tasks:        exec,
		Sessions:     orch,
		Planning:     plan,
		Workspaces:   ws,
		Artifacts:    art,
		TaskChannel:  channel,
		// What a person adds to a task: files, images, audio, links, comments.
		TaskResources: plan,
		TaskComments:  plan,
		PullRequests:  prs,
		Merges:        merges,
		Notifications: notes,
		Usage:         usageSvc,
		LiveCalls:     usageSvc,
		Code:          code,
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
		// The managed claude agents run, and which binary they resolve to.
		ManagedClaude:    managedClaude,
		ManagedClaudeBin: claudeBin,
		// The credential agents sign in with: status only.
		AgentCredential: agentCred.port(),
		// The Sign in with Claude flow that stores it.
		AgentOAuth: agentCred.oauth(),
	}), httpapi.WithToken(cfg.APIToken))
	// Requests slower than the threshold are logged with their query count.
	// An approval waits on a person, so it is never a slow request.
	slow := func(h http.Handler) http.Handler {
		return httpapi.SlowRequestLog(h, httpapi.WithSlowRequestThreshold(cfg.SlowRequestThreshold))
	}
	mux.Handle("/api/", slow(apiRouter))
	mux.Handle(mcpapprove.PathPrefix, mcpapprove.Handler(broker))
	mux.Handle(mcpsession.PathPrefix, slow(mcpsession.TaskHandler(sessionTaskTools(cat, orch, plan, sessions, art, channel, notifier))))
	// The web client owns / and its pages; anything else reaches the legacy
	// designer SPA until it is retired.
	mux.Handle("/", slow(web.NewHandler(web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects, RunCommands: cat.Projects, Documents: plan, History: ws, Code: code, Workspaces: ws, Artifacts: art, ProjectSummaries: cat.Projects, PullRequests: prs,
		Resources: plan, Comments: plan, Notifications: noteReader, GitCredentials: creds.port(), Usage: usageSvc, TokenSavers: cat.Capabilities,
		TokenSaverInstalls: cat.Capabilities, RepositoryIndexes: indexes.port(), Settings: cat.Settings, DataPaths: dataPaths(cfg), SandboxSettings: sandboxStore, SandboxDriver: sandboxDriver(orch), SandboxBuiltin: sandboxBuiltin(orch),
		ManagedClaude: managedClaude, ManagedClaudeBin: claudeBin, AgentCredential: agentCred.port(),
		AgentOAuth: agentCred.oauth()}, assets, uiHandler)))

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

// optionalNotifications is the inbox when NotificationsModule is in the
// graph; graphs built without it (some tests) serve no notifications.
type optionalNotifications struct {
	fx.In

	Service *notifications.Service `optional:"true"`
}

// sessionTaskTools are the tools each session reaches on its own task
// endpoint, /mcp/task/{session}.
func sessionTaskTools(cat catalog.Catalog, orch *orchestration.Service, plan *planning.Service, sessions ports.SessionRepository, art *artifacts.Service, channel *taskchannel.Service, notifier ports.Notifier) []domain.Tool {
	return tooling.SessionTaskTools(plan, sessions, cat.Agents, cat.Architecture,
		tooling.PeerStarter{Sessions: orch, Repositories: cat.Projects},
		tooling.ArtifactTooling{Publisher: art, Attachments: art, ViewURL: httpapi.ArtifactViewPath}, channel,
		tooling.NotificationTooling{Notifier: notifier})
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
