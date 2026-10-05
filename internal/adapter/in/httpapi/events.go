package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// handleEvents streams a project's session changes as server-sent events: one
// `data:` line of ports.SessionChange per change, and a comment every 15s to
// keep the connection alive. The stream ends when the follower fell behind;
// the client then reloads what it shows and connects again.
func (h *Handler) handleEvents(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	ctx := c.Request().Context()
	changes, err := h.orchSvc.FollowProject(ctx, c.QueryParam("project_id"))
	if err != nil {
		return err
	}

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
			data, _ := json.Marshal(change)
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}
