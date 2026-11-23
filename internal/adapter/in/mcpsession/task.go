// Package mcpsession serves the MCP surface a spawned agent session gets for
// itself. Unlike the global MCP server, its tools are scoped to the session that
// calls them: the session id travels in the request path, not in the arguments.
package mcpsession

import (
	"strings"
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

// ModeTaskHandler is TaskHandler for a server whose sessions do not all get
// the same tools: a session for which isProductOwner says so is served
// productOwnerTools and nothing else, so a tool outside the mode is unknown to
// it and a call to it is refused. It reads the session from the request path,
// as the tools do.
func ModeTaskHandler(tools, productOwnerTools []domain.Tool, isProductOwner func(sessionID string) bool) http.Handler {
	general := TaskHandler(tools)
	owner := TaskHandler(productOwnerTools)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, PathPrefix)
		id, _, _ = strings.Cut(id, "/")
		if id != "" && isProductOwner(id) {
			owner.ServeHTTP(w, r)
			return
		}
		general.ServeHTTP(w, r)
	})
}
