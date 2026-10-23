package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// The agent credential (ADR 0018). Every endpoint answers the status only:
// whether one is set, its kind and when it was saved. The value goes in with
// set_agent_credential and never comes back out.

var errAgentCredentialOff = echo.NewHTTPError(http.StatusServiceUnavailable, "agent credential not configured")

func (h *Handler) handleGetAgentCredential(c echo.Context) error {
	if h.agentCredential == nil {
		return errAgentCredentialOff
	}
	return c.JSON(http.StatusOK, h.agentCredential.Status())
}

func (h *Handler) handleSetAgentCredential(c echo.Context) error {
	if h.agentCredential == nil {
		return errAgentCredentialOff
	}
	var in struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	st, err := h.agentCredential.Set(in.Kind, in.Value)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleRemoveAgentCredential(c echo.Context) error {
	if h.agentCredential == nil {
		return errAgentCredentialOff
	}
	st, err := h.agentCredential.Remove()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}
