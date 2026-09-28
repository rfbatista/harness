package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// InteractiveSessionStartedPath is where an interactive session's SessionStart
// hook reports its conversation. The app wires it into the orchestration as a
// URL, so the application layer does not know the route.
const InteractiveSessionStartedPath = "/api/interactive_session_started"

func (h *Handler) handleStartInteractiveSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var req ports.InteractiveRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sess, launch, err := h.orchSvc.StartInteractive(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"session": sess, "launch": launch})
}

func (h *Handler) handleResumeInteractiveSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sess, launch, err := h.orchSvc.ResumeInteractive(c.Request().Context(), req.SessionID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"session": sess, "launch": launch})
}

func (h *Handler) handleEndInteractiveSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var req struct {
		SessionID string `json:"session_id"`
		ExitCode  int    `json:"exit_code"`
		Closed    bool   `json:"closed"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sess, err := h.orchSvc.EndInteractive(c.Request().Context(), req.SessionID, req.ExitCode, req.Closed)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"session": sess})
}

// handleInteractiveSessionStarted receives the claude CLI's SessionStart hook
// input. It always answers 200: the caller is a hook inside someone's live
// session, and a failure here must not surface there. Problems are logged.
func (h *Handler) handleInteractiveSessionStarted(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	if h.orchSvc == nil {
		return c.NoContent(http.StatusOK)
	}
	id := c.QueryParam("session_id")
	var hook struct {
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
	}
	body, _ := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err := json.Unmarshal(body, &hook); err != nil {
		slog.Warn("interactive session hook: bad body", "session", id, "err", err)
		return c.NoContent(http.StatusOK)
	}
	if err := h.orchSvc.RecordClaudeSession(id, hook.SessionID); err != nil {
		slog.Warn("interactive session hook", "session", id, "source", hook.Source, "err", err)
	}
	return c.NoContent(http.StatusOK)
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
