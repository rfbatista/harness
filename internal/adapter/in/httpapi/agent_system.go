package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// The agent system settings (ADR 0011): project defaults and per-repository
// overrides, for the settings UI.

var errAgentSystemOff = echo.NewHTTPError(http.StatusServiceUnavailable, "agent system settings not configured")

// agentSystemView is the project's defaults: every field's value and where it
// comes from.
type agentSystemView struct {
	Effective domain.EffectiveAgentSystem `json:"effective"`
}

func (h *Handler) handleGetProjectAgentSystem(c echo.Context) error {
	if h.agentSystem == nil {
		return errAgentSystemOff
	}
	eff, err := h.agentSystem.Effective(c.Request().Context(), c.Param("project"), "")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, agentSystemView{Effective: eff})
}
