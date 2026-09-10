package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

func (h *Handler) handleListSessions(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	return c.JSON(http.StatusOK, map[string]any{"sessions": h.orchSvc.List(ports.SessionFilter{
		ProjectID: c.QueryParam("project_id"),
		AgentID:   c.QueryParam("agent_id"),
		TicketID:  c.QueryParam("ticket_id"),
		Statuses:  parseStatuses(c.QueryParam("status")),
	})})
}

// parseStatuses reads the `status` query param, which accepts a comma-separated
// list so one UI filter can cover several backend statuses (e.g. "running,idle").
func parseStatuses(raw string) []domain.SessionStatus {
	var out []domain.SessionStatus
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, domain.SessionStatus(s))
		}
	}
	return out
}

func (h *Handler) handleCreateSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var req orchestration.StartRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sess, err := h.orchSvc.Start(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"session": sess})
}

func (h *Handler) handleGetSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	sess := h.orchSvc.Get(c.Param("id"))
	if sess == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"session": sess})
}

func (h *Handler) handleSessionHistory(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var from int64
	if v := c.QueryParam("from_seq"); v != "" {
		from, _ = strconv.ParseInt(v, 10, 64)
	}
	return c.JSON(http.StatusOK, map[string]any{"events": h.orchSvc.History(c.Param("id"), from)})
}

func (h *Handler) handleSessionMessages(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := bindJSON(c, &body); err != nil {
		return err
	}
	if err := h.orchSvc.Send(c.Request().Context(), c.Param("id"), body.Text); err != nil {
		return err
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) handleSessionApproval(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var body struct {
		Allow   bool   `json:"allow"`
		Message string `json:"message"`
		// Answers and Notes are sent only for AskUserQuestion, keyed by the
		// exact question text; multi-select answers are comma-separated. Their
		// presence is what distinguishes answering a question from granting a
		// permission — a denial (skip) carries neither.
		Answers map[string]string `json:"answers"`
		Notes   map[string]string `json:"notes"`
	}
	if err := bindJSON(c, &body); err != nil {
		return err
	}
	id, reqID := c.Param("id"), c.Param("approvalID")
	if body.Allow && len(body.Answers) > 0 {
		if err := h.orchSvc.Answer(c.Request().Context(), id, reqID, body.Answers, body.Notes); err != nil {
			return err
		}
		return c.NoContent(http.StatusAccepted)
	}
	if err := h.orchSvc.Resolve(c.Request().Context(), id, reqID, body.Allow, body.Message); err != nil {
		return err
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) handleSessionAutoRun(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := bindJSON(c, &body); err != nil {
		return err
	}
	if err := h.orchSvc.SetAutoRun(c.Request().Context(), c.Param("id"), body.Enabled); err != nil {
		return err
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) handleSessionStop(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	if err := h.orchSvc.Stop(c.Request().Context(), c.Param("id")); err != nil {
		return err
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) handleDeleteSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	if err := h.orchSvc.Delete(c.Request().Context(), c.Param("id")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleSessionEvents(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	id := c.Param("id")
	w := c.Response()
	ch, replay, cancel := h.orchSvc.Subscribe(id)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	for _, ev := range replayFrom(replay, resumeSeq(c)) {
		writeSSE(w, ev)
	}
	w.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			writeSSE(w, ev)
			w.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev orchestration.SessionEvent) {
	data, _ := json.Marshal(ev)
	// Ephemeral events (streaming deltas) carry no seq and are not resumable, so
	// they omit the SSE id: field. Persisted events always have seq >= 1.
	if ev.Seq > 0 {
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, data)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// resumeSeq resolves the replay high-water mark for an SSE connection.
//
// A browser EventSource cannot set request headers on its first connect, so the
// client passes from_seq as a query param. It cannot set query params on its
// automatic reconnect either, but it does resend Last-Event-ID — which by then
// is the more recent of the two, since the URL's from_seq is frozen at the value
// used for the original connect.
func resumeSeq(c echo.Context) int64 {
	var from int64
	if v := c.QueryParam("from_seq"); v != "" {
		from, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Request().Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			from = n
		}
	}
	return from
}

// replayFrom drops buffered events the client has already seen.
func replayFrom(events []orchestration.SessionEvent, fromSeq int64) []orchestration.SessionEvent {
	if fromSeq <= 0 {
		return events
	}
	out := make([]orchestration.SessionEvent, 0, len(events))
	for _, ev := range events {
		if ev.Seq > fromSeq {
			out = append(out, ev)
		}
	}
	return out
}
