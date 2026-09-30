package testhelper

import (
	"context"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/adapter/in/ui"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/tooling"
)

// StartMCPServer starts an mcp-go server with tools and designer resource on a random port.
// Returns the base URL (e.g. "http://127.0.0.1:12345") and a cleanup function.
func StartMCPServer(t *testing.T, cat *catalog.Catalog, devMode bool) (baseURL string, cleanup func()) {
	return StartMCPServerWithDesigner(t, cat, devMode, nil, "")
}

// StartMCPServerWithDesigner is like StartMCPServer but lets you pass custom embedFS and devServerURL
// for the designer resource. When devMode is false and embedFS is nil, ui.Dist is used.
// When devMode is true and devServerURL is "", ui.DefaultDevServerURL is used.
func StartMCPServerWithDesigner(t *testing.T, cat *catalog.Catalog, devMode bool, embedFS fs.FS, devServerURL string) (baseURL string, cleanup func()) {
	t.Helper()

	toolingSvc := tooling.NewService()
	if cat != nil {
		tooling.Bootstrap(toolingSvc, cat.Tooling(), nil, nil)
	}

	s := server.NewMCPServer("test", "0.0.1", server.WithToolCapabilities(true))
	mcp.RegisterTools(s, toolingSvc)

	designerResource := mcplib.NewResource(ui.DesignerURI, "Designer",
		mcplib.WithResourceDescription("Designer UI"),
		mcplib.WithMIMEType("text/html"),
	)
	designerEmbedFS := embedFS
	if !devMode && designerEmbedFS == nil {
		designerEmbedFS = ui.Dist
	}
	designerDevURL := devServerURL
	if devMode && designerDevURL == "" {
		designerDevURL = ui.DefaultDevServerURL
	}
	s.AddResource(designerResource, func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		if req.Params.URI != ui.DesignerURI {
			return nil, nil
		}
		html, mime, err := ui.DesignerContent(ctx, devMode, designerEmbedFS, designerDevURL)
		if err != nil {
			return nil, err
		}
		return []mcplib.ResourceContents{
			mcplib.TextResourceContents{URI: ui.DesignerURI, MIMEType: mime, Text: html},
		}, nil
	})

	httpSrv := server.NewStreamableHTTPServer(s)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	baseURL = "http://127.0.0.1:" + strconv.Itoa(port)
	srv := &http.Server{Handler: httpSrv}
	go srv.Serve(listener)
	return baseURL, func() { _ = srv.Shutdown(context.Background()) }
}

// NewTestClient creates an mcp-go client and initializes it against the server at baseURL.
func NewTestClient(t *testing.T, baseURL string) *client.Client {
	t.Helper()
	trans, err := transport.NewStreamableHTTP(baseURL)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	c := client.NewClient(trans)
	ctx := context.Background()
	initReq := mcplib.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0.0.1"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		trans.Close()
		t.Fatalf("initialize: %v", err)
	}
	return c
}

// ToolResultText extracts text from the first content item of a tool result.
func ToolResultText(content mcplib.Content) string {
	if tc, ok := content.(mcplib.TextContent); ok {
		return tc.Text
	}
	return ""
}

// ResourceResultText extracts text from the first content item of a ReadResource result.
func ResourceResultText(res *mcplib.ReadResourceResult) string {
	if res == nil || len(res.Contents) == 0 {
		return ""
	}
	if tc, ok := res.Contents[0].(mcplib.TextResourceContents); ok {
		return tc.Text
	}
	return ""
}
