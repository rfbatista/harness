package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// The person's agent attribution (ADR 0063), on the Agent isolation page.
// Both endpoints answer the whole state: the two choices and the JSON that
// agents will get, built by the rule the launch uses.

type agentAttributionOut struct {
	domain.AgentAttribution
	Preview string `json:"preview"`
}

func attributionState(a domain.AgentAttribution) agentAttributionOut {
	return agentAttributionOut{AgentAttribution: a, Preview: a.Preview()}
}

func (h *Handler) handleGetAgentAttribution(c echo.Context) error {
	if h.settings == nil {
		return errSandboxSettingsOff
	}
	all, err := h.settings.GetSettings()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, attributionState(domain.ParseAgentAttribution(all[domain.SettingAgentAttribution])))
}

func (h *Handler) handleSetAgentAttribution(c echo.Context) error {
	if h.settings == nil {
		return errSandboxSettingsOff
	}
	var in domain.AgentAttribution
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	a, err := in.Normalize()
	if err != nil {
		return err
	}
	if _, err := h.settings.UpdateSettings(map[string]string{domain.SettingAgentAttribution: a.Encode()}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, attributionState(a))
}
