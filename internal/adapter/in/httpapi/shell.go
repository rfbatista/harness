package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// The edit proxy's endpoints: a session's hooks and PATH shims ask before
// changing a file of its virtual shell, and report after (contract
// "Harness server ↔ Agent sessions — virtual shell" §5). Like the session
// hook route they are loopback-only and need no token: claude's hooks and
// the shims call them with curl, and the session id in the path scopes them.
const (
	ShellCheckRoute  = "/api/sessions/:id/shell/check"
	ShellCommitRoute = "/api/sessions/:id/shell/commit"
)

func (h *Handler) handleShellCheck(c echo.Context) error {
	return h.shellEdit(c, func(e ports.ShellEdit) ports.ShellAnswer {
		return h.shells.CheckShellEdit(c.Request().Context(), c.Param("id"), e)
	})
}

func (h *Handler) handleShellCommit(c echo.Context) error {
	return h.shellEdit(c, func(e ports.ShellEdit) ports.ShellAnswer {
		return h.shells.CommitShellEdit(c.Request().Context(), c.Param("id"), e)
	})
}

// shellEdit answers {op, paths, tool} with {allow} or {allow: false, code,
// reason}, always 200 once the body parses. A server with no shells has
// nothing inside one, so it allows everything.
func (h *Handler) shellEdit(c echo.Context, answer func(ports.ShellEdit) ports.ShellAnswer) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	var e ports.ShellEdit
	if err := bindJSON(c, &e); err != nil {
		return err
	}
	if h.shells == nil {
		return c.JSON(http.StatusOK, ports.ShellAnswer{Allow: true})
	}
	return c.JSON(http.StatusOK, answer(e))
}
