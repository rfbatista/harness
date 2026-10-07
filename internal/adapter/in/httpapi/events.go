package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// handleEvents streams a project's changes (sessions and tickets) as
// server-sent events: one `data:` line of ports.ProjectChange per change, and
// a comment every 15s to keep the connection alive. The stream ends when the
// follower fell behind; the client then reloads what it shows and connects
// again.
func (h *Handler) handleEvents(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	changes, err := h.orchSvc.FollowProject(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return streamSSE(c, changes, func(ch ports.ProjectChange) any { return ch })
}

// streamSSE writes each value from changes as one `data:` line of JSON, and a
// comment every 15s to keep the connection alive, until the request ends or
// changes closes (the follower fell behind: the client reloads and connects
// again).
func streamSSE[T any](c echo.Context, changes <-chan T, wire func(T) any) error {
	ctx := c.Request().Context()
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case change, ok := <-changes:
			if !ok {
				return nil
			}
			data, _ := json.Marshal(wire(change))
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}
