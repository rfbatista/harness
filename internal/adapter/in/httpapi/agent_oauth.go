package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// The agent OAuth flow (ADR 0018): `claude setup-token` run for the person.
// Every endpoint answers the flow's state, which carries the authorization
// URL but never the token and never the code that was pasted.

var errAgentOAuthOff = echo.NewHTTPError(http.StatusServiceUnavailable, "agent sign-in not configured")

func (h *Handler) handleGetAgentOAuth(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	return c.JSON(http.StatusOK, h.agentOAuth.State())
}

func (h *Handler) handleStartAgentOAuth(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	st, err := h.agentOAuth.Start()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleSubmitAgentOAuthCode(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	st, err := h.agentOAuth.Submit(in.Code)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleCancelAgentOAuth(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	return c.JSON(http.StatusOK, h.agentOAuth.Cancel())
}
