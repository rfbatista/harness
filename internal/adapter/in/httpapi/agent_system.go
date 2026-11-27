package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// The agent system settings (ADR 0011): project defaults and per-repository
// overrides, for the settings UI. A save is a flat object of field path →
// value, or null to reset the field; a refused save answers 422 with
// {errors: [{field, code, message}]}.

var errAgentSystemOff = echo.NewHTTPError(http.StatusServiceUnavailable, "agent system settings not configured")

func (h *Handler) handleGetProjectAgentSystem(c echo.Context) error {
	if h.agentSystem == nil {
		return errAgentSystemOff
	}
	view, err := h.agentSystem.ProjectView(c.Request().Context(), c.Param("project"))
	return agentSystemAnswer(c, view, err)
}

func (h *Handler) handlePatchProjectAgentSystem(c echo.Context) error {
	if h.agentSystem == nil {
		return errAgentSystemOff
	}
	patch, err := agentSystemPatch(c)
	if err != nil {
		return err
	}
	view, err := h.agentSystem.SaveProject(c.Request().Context(), c.Param("project"), patch)
	return agentSystemAnswer(c, view, err)
}

func (h *Handler) handleGetRepositoryAgentSystem(c echo.Context) error {
	if h.agentSystem == nil {
		return errAgentSystemOff
	}
	view, err := h.agentSystem.RepositoryView(c.Request().Context(), c.Param("project"), c.Param("repository"))
	return agentSystemAnswer(c, view, err)
}

func (h *Handler) handlePatchRepositoryAgentSystem(c echo.Context) error {
	if h.agentSystem == nil {
		return errAgentSystemOff
	}
	patch, err := agentSystemPatch(c)
	if err != nil {
		return err
	}
	view, err := h.agentSystem.SaveRepository(c.Request().Context(), c.Param("project"), c.Param("repository"), patch)
	return agentSystemAnswer(c, view, err)
}

// handleImportCheckShGates previews the gate commands the repository's check.sh
// would fill; it saves nothing, so the person reviews them before saving.
func (h *Handler) handleImportCheckShGates(c echo.Context) error {
	if h.agentSystem == nil {
		return errAgentSystemOff
	}
	gates, err := h.agentSystem.CheckShImport(c.Request().Context(), c.Param("project"), c.Param("repository"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"gates": gates})
}

func agentSystemPatch(c echo.Context) (map[string]json.RawMessage, error) {
	var patch map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&patch); err != nil || patch == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "the body must be an object of field path to value"}
	}
	return patch, nil
}

func agentSystemAnswer(c echo.Context, view domain.AgentSystemView, err error) error {
	var ve *domain.AgentSystemValidationError
	switch {
	case errors.As(err, &ve):
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"errors": ve.Errors})
	case err != nil:
		return err
	}
	if view.Values == nil {
		view.Values = domain.AgentSystemValues{}
	}
	return c.JSON(http.StatusOK, view)
}
