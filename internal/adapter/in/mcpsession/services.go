package mcpsession

import (
	"net/http"

	"github.com/rfbatista/harnesskit/mcpbridge"

	"operators-mcp/internal/domain"
)

// ServicesPathPrefix is where the session's services server is served (ADR 0075).
const ServicesPathPrefix = "/mcp/services/"

// ServicesHandler serves the session's services tools at
// /mcp/services/{sessionID}, scoped the way TaskHandler is: the session id
// travels in the request path, and the tools resolve the session's project
// from it.
func ServicesHandler(tools []domain.Tool) http.Handler {
	return mcpbridge.SessionScopedHandler(
		mcpbridge.ServerInfo{Name: "services", Version: "0.0.1"},
		tools,
		ServicesPathPrefix,
	)
}
