package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Token usage as the LLM proxy measured it (see "Spec: harness — token usage
// monitoring and live dashboard").

var errUsageOff = echo.NewHTTPError(http.StatusServiceUnavailable, "usage not configured")

func (h *Handler) handleListUsageRecords(c echo.Context) error {
	if h.usage == nil {
		return errUsageOff
	}
	q := ports.UsageQuery{ProjectID: c.Param("project")}
	if s := c.QueryParam("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "limit must be a number from 1 to 200"}
		}
		if n == 0 {
			n = -1 // an explicit 0 is out of range, not the default
		}
		q.Limit = n
	}
	page, err := h.usage.ListRecentUsage(c.Request().Context(), q)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
}

func (h *Handler) handleUsageSummary(c echo.Context) error {
	if h.usage == nil {
		return errUsageOff
	}
	sum, err := h.usage.UsageSummary(c.Request().Context(), c.Param("project"), ports.UsageRange(c.QueryParam("range")))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, sum)
}

func (h *Handler) handleUsageTotals(c echo.Context) error {
	if h.usage == nil {
		return errUsageOff
	}
	totals, err := h.usage.UsageTotals(c.Request().Context(), ports.UsageTotalsQuery{TaskID: c.QueryParam("task_id"), SessionID: c.QueryParam("session_id")})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, totals)
}

// handleUsageEvents streams each usage record stored for a project as a bare
// `data:` line, in the recent-records shape.
func (h *Handler) handleUsageEvents(c echo.Context) error {
	records, err := h.followUsage(c.Request().Context(), c.Param("project"))
	if err != nil {
		return err
	}
	return h.streamSSE(c, records)
}

func (h *Handler) followUsage(ctx context.Context, projectID string) (<-chan any, error) {
	if h.usage == nil {
		return nil, errUsageOff
	}
	records, err := h.usage.FollowUsage(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return relayAs(ctx, records, func(r *domain.UsageRecord) any { return r }), nil
}

// handleUsageLive godoc
// @Summary Live LLM calls of a project
// @Description Streams, as server-sent events, the LLM calls of a project and the calls of no project that start while the stream is open: `ready` first, then `call.started`, `call.finished` and `calls.missed` events, each with a JSON `data` line. Opening the stream turns call capture on for as long as it is open; nothing is stored and no backlog is sent. A viewer that cannot keep up gets `calls.missed` with a count and stays connected. Credential header values are `[redacted]`.
// @Tags usage
// @Produce text/event-stream
// @Param project path string true "Project id, or _harness for calls of no project"
// @Success 200 {string} string "event stream"
// @Failure 401 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /projects/{project}/usage/live [get]
func (h *Handler) handleUsageLive(c echo.Context) error {
	if h.liveCalls == nil {
		return errUsageOff
	}
	project := c.Param("project")
	viewer := h.liveCalls.OpenLiveCalls(project)
	defer viewer.Close()

	conn, err := openSSE(c, h.streams.timeout())
	if err != nil {
		return nil
	}
	if conn.write(liveEvent(ports.LiveCallEvent{Name: ports.LiveCallReady, Data: map[string]string{"project_id": project}})) != nil {
		return nil
	}
	ctx := c.Request().Context()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-viewer.Ready():
			for ev, ok := viewer.Take(); ok; ev, ok = viewer.Take() {
				if conn.write(liveEvent(ev)) != nil {
					return nil
				}
			}
		case <-ticker.C:
			if conn.ping() != nil {
				return nil
			}
		}
	}
}

func liveEvent(ev ports.LiveCallEvent) []byte {
	data, _ := json.Marshal(ev.Data)
	return fmt.Appendf(nil, "event: %s\ndata: %s\n\n", ev.Name, data)
}
