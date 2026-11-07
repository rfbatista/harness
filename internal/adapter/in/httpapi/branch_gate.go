package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// The branch gate's endpoints: a session's git shim asks for a grant before it
// makes a branch action (contract "Harness server ↔ Agent sessions — branch
// gate" §3). Like the shell and architecture endpoints they are loopback-only
// and need no token: the session id in the path scopes them.
const BranchGateGrantRoute = "/api/sessions/:id/branch_gate/grant"

// SessionBranchGatePath is the path the session's branch gate endpoints are
// under: …/grant.
func SessionBranchGatePath(sessionID string) string {
	return "/api/sessions/" + sessionID + "/branch_gate"
}

func (h *Handler) handleBranchGateGrant(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	var req ports.BranchGrantRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if h.branchGate == nil {
		return c.JSON(http.StatusOK, ports.BranchGrantAnswer{})
	}
	return c.JSON(http.StatusOK, h.branchGate.Grant(c.Request().Context(), c.Param("id"), req))
}
