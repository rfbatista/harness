package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func (h *Handler) handleListSessions(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	sessions, err := h.orchSvc.List(c.Request().Context(), ports.SessionFilter{
		ProjectID: c.QueryParam("project_id"),
		AgentID:   c.QueryParam("agent_id"),
		TicketID:  c.QueryParam("ticket_id"),
		Statuses:  parseStatuses(c.QueryParam("status")),
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"sessions": sessions})
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
	var req ports.StartRequest
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
	sess, err := h.orchSvc.Get(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
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
	events, err := h.orchSvc.History(c.Param("id"), from)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"events": events})
}

// handleSessionTranscript serves a page of the conversation a session's
// claude saved (contract: agent session history, T1–T6).
func (h *Handler) handleSessionTranscript(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	q := ports.TranscriptQuery{Before: c.QueryParam("before"), After: c.QueryParam("after"), Limit: ports.TranscriptDefaultLimit}
	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "limit must be between 1 and 500"}
		}
		q.Limit = n
	}
	page, err := h.orchSvc.SessionTranscript(c.Request().Context(), c.Param("id"), q)
	var se *domain.StructuredError
	if errors.As(err, &se) && se.Code == "SESSION_TRANSCRIPT_MISSING" {
		// 404 here, though a resume answers this code with 409: there is no
		// transcript to read, where a resume conflicts with a session that
		// has none.
		writeCoded(c, http.StatusNotFound, err, se)
		return nil
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
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

	backlog, sent := h.sessionBacklog(id, replay, resumeSeq(c))
	for _, ev := range backlog {
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
				// The hub closed a follower that fell behind, or the session
				// ended: closing the connection lets the client resume from
				// its Last-Event-ID.
				return nil
			}
			if sent.again(ev) {
				continue
			}
			writeSSE(w, ev)
			w.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}

// followSession is a session's events from fromSeq on — the missed ones
// first, then each as it happens — for a tab's stream (stream.go). Its own
// endpoint, handleSessionEvents, also sends each event's seq as the SSE id.
// The result closes when the hub drops the follower, so the stream says end.
func (h *Handler) followSession(ctx context.Context, id string, fromSeq int64) (<-chan any, error) {
	if h.orchSvc == nil {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	ch, replay, cancel := h.orchSvc.Subscribe(id)
	out := make(chan any)
	go func() {
		defer cancel()
		defer close(out)
		send := func(ev ports.SessionEvent) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		backlog, sent := h.sessionBacklog(id, replay, fromSeq)
		for _, ev := range backlog {
			if !send(ev) {
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if sent.again(ev) {
					continue
				}
				if !send(ev) {
					return
				}
			}
		}
	}()
	return out, nil
}

// sessionBacklog is what a follower resuming after fromSeq missed, taken
// right after it subscribed: the hub's replay when it reaches back far
// enough, else the stored log, which outlives the replay ring and the
// session. The seqs it holds are returned too, so the same events arriving
// live are not sent twice.
func (h *Handler) sessionBacklog(id string, replay []ports.SessionEvent, fromSeq int64) ([]ports.SessionEvent, seqSet) {
	backlog := replayFrom(replay, fromSeq)
	if fromSeq > 0 && (len(replay) == 0 || replay[0].Seq > fromSeq+1) {
		stored, err := h.orchSvc.History(id, fromSeq)
		if err != nil {
			slog.Warn("session backlog from the log", "session", id, "from_seq", fromSeq, "err", err)
		} else {
			var last int64
			if len(stored) > 0 {
				last = stored[len(stored)-1].Seq
			}
			backlog = append(stored, replayFrom(replay, last)...)
		}
	}
	sent := seqSet{}
	for _, ev := range backlog {
		if ev.Seq > 0 {
			sent[ev.Seq] = struct{}{}
		}
	}
	return backlog, sent
}

// seqSet holds the persisted seqs a follower already got from its backlog.
type seqSet map[int64]struct{}

// again reports whether a live event was already sent with the backlog.
// Ephemeral events carry no seq and are never repeats.
func (s seqSet) again(ev ports.SessionEvent) bool {
	if ev.Seq <= 0 {
		return false
	}
	_, ok := s[ev.Seq]
	return ok
}

func writeSSE(w http.ResponseWriter, ev ports.SessionEvent) {
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
func replayFrom(events []ports.SessionEvent, fromSeq int64) []ports.SessionEvent {
	if fromSeq <= 0 {
		return events
	}
	out := make([]ports.SessionEvent, 0, len(events))
	for _, ev := range events {
		if ev.Seq > fromSeq {
			out = append(out, ev)
		}
	}
	return out
}
