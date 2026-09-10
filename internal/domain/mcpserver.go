package domain

// MCP server configuration is implemented by
// github.com/rfbatista/harnesskit/mcpserver. These are type aliases, so
// domain.MCPServer and mcpserver.Server are the same type.

import "github.com/rfbatista/harnesskit/mcpserver"

type (
	// MCPServer is the configuration of an external MCP server that can be
	// managed via the UI. Transport is stdio, sse, or streamable-http.
	MCPServer = mcpserver.Server
	// MCPProbeResult is the outcome of probing an MCP server connection.
	MCPProbeResult = mcpserver.ProbeResult
	// MCPServerInput holds the persistable fields of an MCP server for create
	// and update.
	MCPServerInput = mcpserver.Input
)

// Transports an MCP server can speak.
const (
	MCPTransportStdio          = mcpserver.TransportStdio
	MCPTransportSSE            = mcpserver.TransportSSE
	MCPTransportStreamableHTTP = mcpserver.TransportStreamableHTTP
)

// Outcomes of the last probe of an MCP server.
const (
	MCPProbeStatusUnknown = mcpserver.ProbeStatusUnknown
	MCPProbeStatusOK      = mcpserver.ProbeStatusOK
	MCPProbeStatusError   = mcpserver.ProbeStatusError
)

// Strategies for handling a name collision during an import.
const (
	ImportDuplicateSkip   = mcpserver.ImportDuplicateSkip
	ImportDuplicateUpdate = mcpserver.ImportDuplicateUpdate
	ImportDuplicateRename = mcpserver.ImportDuplicateRename
)

// NormalizeMCPTransport maps aliases to canonical transport names.
func NormalizeMCPTransport(transport string) string {
	return mcpserver.NormalizeTransport(transport)
}

// ValidateMCPServer checks transport and required fields on an MCP server
// config. It mutates m: the transport is canonicalized and the name trimmed in
// place, which callers depend on.
func ValidateMCPServer(m *MCPServer) error { return mcpserver.Validate(m) }

// MCPServerNameConflict reports whether name is already taken
// (case-insensitively), ignoring excludeID.
func MCPServerNameConflict(servers []*MCPServer, name, excludeID string) bool {
	return mcpserver.NameConflict(servers, name, excludeID)
}

// ParseCursorMCPConfig parses a Cursor-style mcp.json into server configs.
func ParseCursorMCPConfig(content string) ([]MCPServer, error) {
	return mcpserver.ParseCursorConfig(content)
}

// UniqueMCPName returns name, or name-2 … name-999, avoiding a collision.
func UniqueMCPName(servers []*MCPServer, name string) string {
	return mcpserver.UniqueName(servers, name)
}
