package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// The Agent board across every project (see "Contract: harness server ↔ web
// UI — the agent board seed and the live session feed").

var errAgentBoardOff = echo.NewHTTPError(http.StatusServiceUnavailable, "agent board not configured")

func (h *Handler) handleAgentBoard(c echo.Context) error {
	if h.agentBoard == nil {
		return errAgentBoardOff
	}
	seed, err := h.agentBoard.Seed(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, seed)
}

// handleLiveSessionEvents streams the live session feed as bare `data:` lines
// of domain.BoardEvent.
func (h *Handler) handleLiveSessionEvents(c echo.Context) error {
	events, err := h.followLiveSessions(c.Request().Context())
	if err != nil {
		return err
	}
	return h.streamSSE(c, events)
}

func (h *Handler) followLiveSessions(ctx context.Context) (<-chan any, error) {
	if h.agentBoard == nil {
		return nil, errAgentBoardOff
	}
	events, err := h.agentBoard.Follow(ctx)
	if err != nil {
		return nil, err
	}
	return relayAs(ctx, events, func(ev domain.BoardEvent) any { return ev }), nil
}
