// Package mcpbench serves a bench run's mock tool layer as an MCP server (ADR
// 0015): the one server a bench run's CLI is given, in place of the harness's
// and the project's.
package mcpbench

import (
	"context"
	"net"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"operators-mcp/internal/application/bench"
)

// Name is the server's name in the CLI's MCP configuration.
const Name = "bench"

// anyArguments is the input schema of every mock tool: a case records what the
// agent passes, whatever its shape.
var anyArguments = []byte(`{"type":"object","additionalProperties":true}`)

// Serve offers the layer's tools on a loopback port and answers its URL; stop
// closes the listener.
func Serve(layer *bench.MockLayer) (url string, stop func(), err error) {
	mocked := map[string]bool{}
	for _, t := range layer.Tools() {
		mocked[t.Name] = true
	}
	// The server refuses a tool it does not list before any handler runs, so a
	// call to an unmocked one is recorded here; the server answers the error.
	hooks := &server.Hooks{}
	hooks.AddBeforeCallTool(func(_ context.Context, _ any, req *mcp.CallToolRequest) {
		if !mocked[req.Params.Name] {
			layer.Call(req.Params.Name, req.GetArguments())
		}
	})
	s := server.NewMCPServer(Name, "0.0.1", server.WithToolCapabilities(false), server.WithHooks(hooks))
	for _, t := range layer.Tools() {
		s.AddTool(mcp.NewToolWithRawSchema(t.Name, t.Description, anyArguments), handler(layer))
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	stream := server.NewStreamableHTTPServer(s, server.WithStateLess(true))
	mux := http.NewServeMux()
	mux.Handle("/mcp", stream)
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(l) }()
	return "http://" + l.Addr().String() + "/mcp", func() { _ = srv.Shutdown(context.Background()) }, nil
}

func handler(layer *bench.MockLayer) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res := layer.Call(req.Params.Name, req.GetArguments())
		if res.IsError {
			return mcp.NewToolResultError(res.Text), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	}
}
