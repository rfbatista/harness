package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/in/ui"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/execution"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/workspaces"
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
func registerHTTPServer(lc fx.Lifecycle, cfg Config, cat catalog.Catalog, appRunner *apps.Service, ts *tooling.Service, exec *execution.Service, orch *orchestration.Service, plan *planning.Service, ws *workspaces.Service, broker *approval.Broker, sessions ports.SessionRepository, art *artifacts.Service, channel *taskchannel.Service) error {
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

	mux := http.NewServeMux()
	apiRouter := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects:     cat.Projects,
		Discovery:    cat.Projects,
		Env:          cat.Projects,
		RunCommands:  cat.Projects,
		Apps:         appRunner,
		Architecture: cat.Architecture,
		Agents:       cat.Agents,
		Capabilities: cat.Capabilities,
		Settings:     cat.Settings,
		Tools:        ts,
		Tasks:        exec,
		Sessions:     orch,
		Planning:     plan,
		Workspaces:   ws,
		Artifacts:    art,
	}), httpapi.WithToken(cfg.APIToken))
	mux.Handle("/api/", apiRouter)
	mux.Handle(mcpapprove.PathPrefix, mcpapprove.Handler(broker))
	mux.Handle(mcpsession.PathPrefix, mcpsession.TaskHandler(tooling.SessionTaskTools(plan, sessions, cat.Agents, cat.Architecture,
		tooling.PeerStarter{Sessions: orch, Repositories: cat.Projects},
		tooling.ArtifactTooling{Publisher: art, ViewURL: httpapi.ArtifactViewPath}, channel)))
	// The web client owns / and its pages; anything else reaches the legacy
	// designer SPA until it is retired.
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects, Documents: plan, History: ws}, assets, uiHandler))

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: httpapi.CORSMiddleware(mux)}
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
