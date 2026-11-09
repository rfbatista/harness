package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The branch gate's endpoints (contract "Harness server ↔ Agent sessions —
// branch gate" §2 and §3, and "Harness server ↔ Web UI — branch requests and
// the Branches list" §2). A session's hook and git shim ask the loopback-only
// ones; they need no token, the session id in the path scopes them. Answering
// is the person's, from a browser: no session task tool does it.
const (
	BranchGateRoute      = "/api/sessions/:id/branch_gate"
	BranchGateGrantRoute = "/api/sessions/:id/branch_gate/grant"
	// BranchGatePushedRoute is where the shim says a push it let through succeeded.
	BranchGatePushedRoute = "/api/sessions/:id/branch_gate/pushed"
)

// SessionBranchGatePath is the path the session's branch gate endpoints are
// under: the hook at it, the shim's …/grant below it.
func SessionBranchGatePath(sessionID string) string {
	return "/api/sessions/" + sessionID + "/branch_gate"
}

// handleBranchGateHook answers claude's PreToolUse hook input, holding the
// answer while the person decides.
func (h *Handler) handleBranchGateHook(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	if c.QueryParam("hook") != "PreToolUse" {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "hook must be PreToolUse"}
	}
	var in ports.BranchHookInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if h.branchGate == nil {
		return c.NoContent(http.StatusOK)
	}
	v := h.branchGate.Hook(c.Request().Context(), c.Param("id"), in)
	if v.Decision == "" {
		return c.NoContent(http.StatusOK)
	}
	out := map[string]any{"hookEventName": "PreToolUse", "permissionDecision": v.Decision}
	if v.Reason != "" {
		out["permissionDecisionReason"] = v.Reason
	}
	if v.Context != "" {
		out["additionalContext"] = v.Context
	}
	return c.JSON(http.StatusOK, map[string]any{"hookSpecificOutput": out})
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

func (h *Handler) handleBranchGatePushed(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	var in ports.BranchPushed
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if h.branchGate == nil {
		return c.NoContent(http.StatusOK)
	}
	if err := h.branchGate.Pushed(c.Request().Context(), c.Param("id"), in); err != nil {
		return err
	}
	return c.NoContent(http.StatusOK)
}

func (h *Handler) handleBranchRequests(c echo.Context) error {
	if h.branchGate == nil {
		return c.JSON(http.StatusOK, map[string]any{"requests": []domain.BranchRequest{}})
	}
	list, err := h.branchGate.ListBranchRequests(c.Request().Context(), domain.BranchRequestFilter{
		SessionID: c.QueryParam("session_id"), TaskID: c.QueryParam("task_id"), State: domain.BranchRequestState(c.QueryParam("state")),
	})
	if err != nil {
		return err
	}
	if list == nil {
		list = []domain.BranchRequest{}
	}
	return c.JSON(http.StatusOK, map[string]any{"requests": list})
}

func (h *Handler) handleAnswerBranchRequest(c echo.Context) error {
	var in struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if h.branchGate == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "the branch gate is not available"}
	}
	r, err := h.branchGate.Answer(c.Request().Context(), c.Param("id"), in.Decision, in.Note)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"request": r})
}
