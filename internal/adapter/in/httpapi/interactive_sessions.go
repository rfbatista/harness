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

// InteractiveSessionHookPath is where an interactive session's hooks report:
// SessionStart its conversation, UserPromptSubmit and Stop its turns. The app
// wires it into the orchestration as a URL, so the application layer does not
// know the route.
const InteractiveSessionHookPath = "/api/interactive_session_hook"

// InteractiveSessionStartedPath is the SessionStart-only route sessions
// launched before the turn hooks still call.
const InteractiveSessionStartedPath = "/api/interactive_session_started"

func (h *Handler) handleStartInteractiveSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var req ports.InteractiveRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sess, spec, err := h.orchSvc.StartInteractive(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"session": sess, "agent": spec})
}

func (h *Handler) handleResumeInteractiveSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var req ports.ResumeRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sess, spec, err := h.orchSvc.ResumeInteractive(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"session": sess, "agent": spec})
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

// handleInteractiveSessionHook receives the claude CLI's hook input; the
// event query parameter names the hook (SessionStart when absent, as on the
// older route). It always answers 200: the caller is a hook inside someone's
// live session, and a failure here must not surface there. Problems are
// logged. Only Stop gets a body: the turns the session goes on with, as the
// hook's decision.
func (h *Handler) handleInteractiveSessionHook(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	if h.orchSvc == nil {
		return c.NoContent(http.StatusOK)
	}
	ctx := c.Request().Context()
	id, event := c.QueryParam("session_id"), c.QueryParam("event")
	var hook struct {
		SessionID      string `json:"session_id"`
		Source         string `json:"source"`
		StopHookActive bool   `json:"stop_hook_active"`
	}
	body, _ := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err := json.Unmarshal(body, &hook); err != nil {
		slog.Warn("interactive session hook: bad body", "session", id, "event", event, "err", err)
		return c.NoContent(http.StatusOK)
	}
	switch event {
	case "", "SessionStart":
		if err := h.orchSvc.RecordClaudeSession(ctx, id, hook.SessionID); err != nil {
			slog.Warn("interactive session hook", "session", id, "source", hook.Source, "err", err)
		}
	case "UserPromptSubmit":
		if err := h.orchSvc.TurnStarted(ctx, id); err != nil {
			slog.Warn("interactive session hook", "session", id, "event", event, "err", err)
		}
	case "Stop":
		next, err := h.orchSvc.TurnEnded(ctx, id, hook.StopHookActive)
		if err != nil {
			slog.Warn("interactive session hook", "session", id, "event", event, "err", err)
		}
		if next != "" {
			return c.JSON(http.StatusOK, map[string]string{"decision": "block", "reason": next})
		}
	default:
		slog.Warn("interactive session hook: unknown event", "session", id, "event", event)
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
