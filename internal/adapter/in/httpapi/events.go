package httpapi

import (
	"context"
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
	changes, err := h.followProject(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return h.streamSSE(c, changes)
}

func (h *Handler) followProject(ctx context.Context, projectID string) (<-chan any, error) {
	if h.orchSvc == nil {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	changes, err := h.orchSvc.FollowProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return relayAs(ctx, changes, func(ch ports.ProjectChange) any { return ch }), nil
}

// streamSSE writes each value from changes as one `data:` line of JSON, and a
// comment every 15s to keep the connection alive, until the request ends,
// changes closes (the follower fell behind: the client reloads and connects
// again) or a write fails or times out (a dead or stuck client).
func (h *Handler) streamSSE(c echo.Context, changes <-chan any) error {
	ctx := c.Request().Context()
	conn, err := openSSE(c, h.streams.timeout())
	if err != nil {
		return nil
	}

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
			if conn.write(fmt.Appendf(nil, "data: %s\n\n", data)) != nil {
				return nil
			}
		case <-ticker.C:
			if conn.ping() != nil {
				return nil
			}
		}
	}
}
