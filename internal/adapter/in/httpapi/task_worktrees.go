package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// The architect's worktree read hook's endpoints (contract "Harness server ↔
// Agent sessions — architect worktree visibility"): TaskWorktreesRoute is the
// lookup it asks. Like the other hook endpoints they are loopback-only and need
// no token: the session id in the path scopes them.
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
	rec, ok := h.archRules.(ports.WorktreeReadRecorder)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "worktree read denials are not recorded here")
	}
	if err := rec.RecordWorktreeRead(c.Request().Context(), c.Param("id"), in.Tool, in.Path, in.Reason); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
