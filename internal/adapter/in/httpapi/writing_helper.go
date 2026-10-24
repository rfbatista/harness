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

// The writing helper's step (ADR 0026). The answer streams SSE-framed events
// (`event: <name>` and one `data:` line of JSON): delta, then result, or
// error. A refusal that comes before the first event is a plain JSON error.
// The browser aborting the request is Stop: the request context ends, which
// kills the claude process.

const writingHelperUnavailable = "WRITING_HELPER_UNAVAILABLE"

func (h *Handler) handleRunWritingHelperStep(c echo.Context) error {
	if h.writingHelper == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "writing helper not configured")
	}
	var req ports.WritingHelperRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	var conn *sseConn
	emit := func(ev ports.WritingHelperEvent) {
		if conn == nil {
			var err error
			if conn, err = openSSE(c, h.streams.timeout()); err != nil {
				conn = &sseConn{} // a dead client: the step ends with its context
				return
			}
		}
		if conn.w == nil {
			return
		}
		data, _ := json.Marshal(ev.Data)
		_ = conn.write(fmt.Appendf(nil, "event: %s\ndata: %s\n\n", ev.Name, data))
	}
	err := h.writingHelper.Run(ctx, req, emit)
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return nil
	}
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		if conn != nil {
			emit(ports.WritingHelperEvent{Name: "error", Data: errorEvent{Code: "WRITING_HELPER_FAILED", Message: "the step failed"}})
			return nil
		}
		return err
	}
	if conn == nil && se.Code != "WRITING_HELPER_FAILED" {
		return h.writingHelperRefusal(c, err, se)
	}
	emit(ports.WritingHelperEvent{Name: "error", Data: errorEvent{Code: se.Code, Message: se.Message, FixURL: fixURLOf(err)}})
	return nil
}

type errorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	FixURL  string `json:"fix_url,omitempty"`
}

// fixURLOf is where an unavailable helper's error says to fix it, "" for any
// other error.
func fixURLOf(err error) string {
	var de *domain.DetailedError
	if errors.As(err, &de) {
		if d, ok := de.Details.(map[string]string); ok {
			return d["fix_url"]
		}
	}
	return ""
}

// writingHelperRefusal answers a refusal as the usual JSON error. An
// unavailable helper is a 503 that adds where to fix it.
func (h *Handler) writingHelperRefusal(c echo.Context, err error, se *domain.StructuredError) error {
	if se.Code != writingHelperUnavailable {
		return err
	}
	body := errorBody(se.Message, se.Code)
	if fix := fixURLOf(err); fix != "" {
		body["fix_url"] = fix
	}
	return c.JSON(http.StatusServiceUnavailable, body)
}
