package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// A session's interviews (see "Contract: harness server ↔ web UI —
// interviews").

var errInterviewsOff = echo.NewHTTPError(http.StatusServiceUnavailable, "interviews not configured")

// handleSessionInterviews answers the session's interviews and the number of
// its feed they are current to, the body the session screen's seed carries.
func (h *Handler) handleSessionInterviews(c echo.Context) error {
	if h.interviews == nil {
		return errInterviewsOff
	}
	id := c.Param("id")
	lists, err := h.interviews.ListInterviews(c.Request().Context(), []string{id})
	if err != nil {
		return err
	}
	list := lists[id]
	if list == nil {
		list = &domain.InterviewList{Interviews: []*domain.Interview{}}
	}
	return c.JSON(http.StatusOK, list)
}

// handleSessionInterviewEvents streams a session's interview events as bare
// `data:` lines of domain.InterviewEvent.
func (h *Handler) handleSessionInterviewEvents(c echo.Context) error {
	events, err := h.followInterviews(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return h.streamSSE(c, events)
}

func (h *Handler) followInterviews(ctx context.Context, sessionID string) (<-chan any, error) {
	if h.interviews == nil {
		return nil, errInterviewsOff
	}
	events, err := h.interviews.FollowInterviews(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return relayAs(ctx, events, func(ev domain.InterviewEvent) any { return ev }), nil
}
