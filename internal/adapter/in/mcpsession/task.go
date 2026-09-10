// Package mcpsession serves the MCP surface a spawned agent session gets for
// itself. Unlike the global MCP server, its tools are scoped to the session that
// calls them: the session id travels in the request path, not in the arguments.
package mcpsession

import (
	"net/http"

	"github.com/rfbatista/harnesskit/mcpbridge"

	"operators-mcp/internal/domain"
)

const PathPrefix = "/mcp/task/"

// TaskHandler serves the session's task tools at /mcp/task/{sessionID}. The
// session id is read from the request path into the context, where the tools
// pick it up; the streamable server (used as an http.Handler) serves regardless
// of path.
//
// One handler serves every session; isolation is entirely that path-to-context
// hop, so the tools must fail closed on an empty id.
func TaskHandler(tools []domain.Tool) http.Handler {
	return mcpbridge.SessionScopedHandler(
		mcpbridge.ServerInfo{Name: "task", Version: "0.0.1"},
		tools,
		PathPrefix,
	)
}
