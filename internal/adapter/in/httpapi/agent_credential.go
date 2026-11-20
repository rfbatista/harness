package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
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

// The credential one project's agents sign in with, instead of the global one
// (ADR 0018). Same rule: status only, the value never comes back out. Status
// is about the project's own credential; without one its agents use the global.

// projectCredential answers the port and the project id every route needs; the
// project must exist, so a credential is never kept for nothing.
func (h *Handler) projectCredential(c echo.Context, projectID string) (ports.ProjectAgentCredentials, error) {
	if h.projectAgentCredential == nil {
		return nil, errAgentCredentialOff
	}
	if h.projects != nil {
		if _, err := h.projects.GetProject(c.Request().Context(), projectID); err != nil {
			return nil, err
		}
	}
	return h.projectAgentCredential, nil
}

func (h *Handler) handleGetProjectAgentCredential(c echo.Context) error {
	id := c.QueryParam("project_id")
	svc, err := h.projectCredential(c, id)
	if err != nil {
		return err
	}
	st, err := svc.StatusFor(id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleSetProjectAgentCredential(c echo.Context) error {
	var in struct {
		ProjectID string `json:"project_id"`
		Kind      string `json:"kind"`
		Value     string `json:"value"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	svc, err := h.projectCredential(c, in.ProjectID)
	if err != nil {
		return err
	}
	st, err := svc.SetFor(in.ProjectID, in.Kind, in.Value)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleRemoveProjectAgentCredential(c echo.Context) error {
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	svc, err := h.projectCredential(c, in.ProjectID)
	if err != nil {
		return err
	}
	st, err := svc.RemoveFor(in.ProjectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}
