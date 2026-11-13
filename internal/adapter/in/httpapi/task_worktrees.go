package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// TaskWorktreesRoute is the lookup an architect's worktree read hook asks
// (contract "Harness server ↔ Agent sessions — architect worktree
// visibility"). Like the other hook endpoints it is loopback-only and needs
// no token: the session id in the path scopes it.
const (
	TaskWorktreesRoute = "/api/sessions/:id/task_worktrees"
	// TaskWorktreeDenialsRoute is where the hook reports a call it denied, so
	// the harness records it with the session (hook worktree_read).
	TaskWorktreeDenialsRoute = "/api/sessions/:id/task_worktrees/denials"
)

// SessionTaskWorktreesPath is the lookup's path for a session.
func SessionTaskWorktreesPath(sessionID string) string {
	return "/api/sessions/" + sessionID + "/task_worktrees"
}

func (h *Handler) handleTaskWorktrees(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	lookup, ok := h.orchSvc.(ports.TaskWorktrees)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "task worktrees are not available")
	}
	list, err := lookup.TaskWorktrees(c.Request().Context(), c.Param("id"), c.QueryParam("path"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, list)
}

func (h *Handler) handleTaskWorktreeDenial(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	var in struct {
		Tool   string `json:"tool"`
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if rec, ok := h.archRules.(ports.WorktreeReadRecorder); ok {
		if err := rec.RecordWorktreeRead(c.Request().Context(), c.Param("id"), in.Tool, in.Path, in.Reason); err != nil {
			return err
		}
	}
	return c.NoContent(http.StatusNoContent)
}
