package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The architect channel, as the person sees it (see "Contract: Harness
// server ↔ Web UI — architect channel API").

func (h *Handler) channelOr503() error {
	if h.channel == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "the architect channel is not configured")
	}
	return nil
}

func invalidInput(msg string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg}
}

// handleTaskMessages lists a task's messages, oldest first.
func (h *Handler) handleTaskMessages(c echo.Context) error {
	if err := h.channelOr503(); err != nil {
		return err
	}
	f := ports.TaskMessageFilter{SessionID: c.QueryParam("session_id")}
	if since := c.QueryParam("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return invalidInput("since must be an RFC 3339 time")
		}
		f.Since = t
	}
	msgs, err := h.channel.ListTaskMessages(c.Request().Context(), c.QueryParam("ticket_id"), f)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"messages": msgs})
}

// handleReviewRequests lists a task's review requests, or a project's in one
// state (the board's inbox), newest first.
func (h *Handler) handleReviewRequests(c echo.Context) error {
	if err := h.channelOr503(); err != nil {
		return err
	}
	state, err := domain.ParseReviewState(c.QueryParam("state"))
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var reviews []*domain.ReviewRequest
	switch ticket, project := c.QueryParam("ticket_id"), c.QueryParam("project_id"); {
	case ticket != "":
		reviews, err = h.channel.ListReviewRequests(ctx, ticket, state)
	case project != "":
		reviews, err = h.channel.ListProjectReviewRequests(ctx, project, state)
	default:
		return invalidInput("ticket_id or project_id is required")
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"review_requests": reviews})
}

// handleRespondReviewRequest settles a pending review with the person's
// decision and delivers it to the architect.
func (h *Handler) handleRespondReviewRequest(c echo.Context) error {
	if err := h.channelOr503(); err != nil {
		return err
	}
	var in struct {
		ReviewID string `json:"review_id"`
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	r, delivered, err := h.channel.RespondReview(c.Request().Context(), in.ReviewID, domain.ReviewState(in.Decision), in.Note)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"review_request": r, "delivered": delivered})
}

// handleSetStatusCheck pauses (0), resumes or retunes a delegate's loop.
func (h *Handler) handleSetStatusCheck(c echo.Context) error {
	if err := h.channelOr503(); err != nil {
		return err
	}
	var in struct {
		DelegateSessionID string `json:"delegate_session_id"`
		EveryMinutes      *int   `json:"every_minutes"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.EveryMinutes == nil {
		return invalidInput("every_minutes is required: 0 pauses, 2–240 resumes or retunes")
	}
	check, err := h.channel.SetStatusCheckByPerson(c.Request().Context(), in.DelegateSessionID, *in.EveryMinutes)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"status_check": check})
}
