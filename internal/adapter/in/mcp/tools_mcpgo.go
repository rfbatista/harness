package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	"github.com/rfbatista/harnesskit/mcpbridge"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Schema and error conversion live in harnesskit/mcpbridge. These wrappers keep
// this package as the single place the rest of the app reaches mcp-go through.

// RegisterTools registers all tools from the tooling registry on the mcp-go server.
func RegisterTools(s *server.MCPServer, toolingSvc ports.ToolRegistry) {
	mcpbridge.Register(s, toolingSvc.List())
}

// RegisterDomainTools registers a plain slice of domain tools on the mcp-go
// server. It exists for the tool surfaces that are not in the global registry —
// the per-session servers — so they share the same conversion.
func RegisterDomainTools(s *server.MCPServer, tools []domain.Tool) {
	mcpbridge.Register(s, tools)
}
