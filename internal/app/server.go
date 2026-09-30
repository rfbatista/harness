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
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/execution"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
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
func registerHTTPServer(lc fx.Lifecycle, cfg Config, cat catalog.Catalog, ts *tooling.Service, exec *execution.Service, orch *orchestration.Service, plan *planning.Service, ws *workspaces.Service, broker *approval.Broker, sessions ports.SessionRepository) error {
	uiHandler, err := ui.SPAHandler(ui.Dist)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	apiRouter := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects:     cat.Projects,
		Architecture: cat.Architecture,
		Agents:       cat.Agents,
		Capabilities: cat.Capabilities,
		Settings:     cat.Settings,
		Tools:        ts,
		Tasks:        exec,
		Sessions:     orch,
		Planning:     plan,
		Workspaces:   ws,
	}))
	mux.Handle("/api/", apiRouter)
	mux.Handle(mcpapprove.PathPrefix, mcpapprove.Handler(broker))
	mux.Handle(mcpsession.PathPrefix, mcpsession.TaskHandler(tooling.SessionTaskTools(plan, sessions)))
	mux.Handle("/", uiHandler)

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
			slog.Info("HTTP server listening", "addr", srv.Addr, "ui", "/", "api", "/api")
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
