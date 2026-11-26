package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A session's Bash hooks tell the server when a call starts and ends, so the
// answer to its PostToolUse can carry the reason of an encerramento por memória
// that happened meanwhile (ADR 0006). Loopback only, like the other session
// hooks: the session id in the path scopes it.
const MemoryHookRoute = "/api/sessions/:id/memory/hook"

// SessionMemoryPath is the path the session's memory endpoints are under.
func SessionMemoryPath(sessionID string) string { return "/api/sessions/" + sessionID + "/memory" }

func (h *Handler) handleMemoryHook(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	event := c.QueryParam("hook")
	if event != "PreToolUse" && event != "PostToolUse" {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "hook must be PreToolUse or PostToolUse"}
	}
	var in struct {
		ToolUseID string `json:"tool_use_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	notices, ok := h.orchSvc.(ports.MemoryNotices)
	if !ok || in.ToolUseID == "" {
		return c.NoContent(http.StatusOK)
	}
	if event == "PreToolUse" {
		notices.BashStarted(c.Param("id"), in.ToolUseID)
		return c.NoContent(http.StatusOK)
	}
	return hookAnswer(c, event, ports.ArchitectureVerdict{Context: notices.BashEnded(c.Param("id"), in.ToolUseID)})
}

// handleSessionMemory is GET /api/sessions/:id/memory: the last session.memory
// sample of a live session, plus "kills" for its run (Contract: memória da
// sessão §3); 404 when the session is not live.
func (h *Handler) handleSessionMemory(c echo.Context) error {
	reader, ok := h.orchSvc.(ports.MemoryReader)
	if !ok {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "memory is not available")
	}
	read, live := reader.SessionMemory(c.Param("id"))
	if !live {
		return echo.NewHTTPError(http.StatusNotFound, "session is not live")
	}
	return c.JSON(http.StatusOK, read)
}
