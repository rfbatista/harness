package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The architecture assistant's turn (ADR 0060). The answer streams SSE-framed
// events (`event: <name>` and one `data:` line of JSON): conversation first,
// then activity and delta, then result, or error. A refusal that comes before
// the first event is a plain JSON error. The browser aborting the request is
// Stop: the request context ends, which kills the claude process.

const (
	architectureAssistantUnavailable = "ARCHITECTURE_ASSISTANT_UNAVAILABLE"
	architectureAssistantFailed      = "ARCHITECTURE_ASSISTANT_FAILED"
)

func (h *Handler) handleRunArchitectureAssistantTurn(c echo.Context) error {
	if h.architectureAssistant == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "architecture assistant not configured")
	}
	var req ports.ArchitectureAssistantRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var conn *sseConn
	emit := func(ev ports.ArchitectureAssistantEvent) {
		if conn == nil {
			var err error
			if conn, err = openSSE(c, h.streams.timeout()); err != nil {
				conn = &sseConn{} // a dead client: the turn ends with its context
				return
			}
		}
		if conn.w == nil {
			return
		}
		data, _ := json.Marshal(ev.Data)
		_ = conn.write(fmt.Appendf(nil, "event: %s\ndata: %s\n\n", ev.Name, data))
	}
	err := h.architectureAssistant.Run(ctx, req, emit)
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return nil
	}
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		if conn != nil {
			emit(ports.ArchitectureAssistantEvent{Name: "error", Data: errorEvent{Code: architectureAssistantFailed, Message: "the turn failed"}})
			return nil
		}
		return err
	}
	if conn == nil && se.Code != architectureAssistantFailed {
		return architectureAssistantRefusal(c, err, se)
	}
	emit(ports.ArchitectureAssistantEvent{Name: "error", Data: errorEvent{Code: se.Code, Message: se.Message, FixURL: fixURLOf(err)}})
	return nil
}

// architectureAssistantRefusal answers a refusal as the usual JSON error. An
// unavailable assistant is a 503 that adds where to fix it; the other refusals
// are mapped by the error handler.
func architectureAssistantRefusal(c echo.Context, err error, se *domain.StructuredError) error {
	if se.Code != architectureAssistantUnavailable {
		return err
	}
	body := errorBody(se.Message, se.Code)
	if fix := fixURLOf(err); fix != "" {
		body["fix_url"] = fix
	}
	return c.JSON(http.StatusServiceUnavailable, body)
}

// handleEndArchitectureAssistantConversation forgets a conversation. It answers
// 204 whether the id exists or not, and reads the body whatever its content
// type, because navigator.sendBeacon posts text/plain.
func (h *Handler) handleEndArchitectureAssistantConversation(c echo.Context) error {
	if h.architectureAssistant == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "architecture assistant not configured")
	}
	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.ConversationID != "" {
		h.architectureAssistant.End(req.ConversationID)
	}
	return c.NoContent(http.StatusNoContent)
}
