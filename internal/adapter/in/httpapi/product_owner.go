package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The product owner's API (contract "Harness server ↔ Web UI — product
// owner"): settings and threads.

func (h *Handler) productOwnerReady() error {
	if h.productOwner == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "the product owner is not available on this server"}
	}
	return nil
}

func (h *Handler) handleGetProductOwner(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	v, err := h.productOwner.Settings(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (h *Handler) handleUpdateProductOwner(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	var in struct {
		ProjectID  string  `json:"project_id"`
		Enabled    *bool   `json:"enabled"`
		AgentID    *string `json:"agent_id"`
		Prompt     *string `json:"prompt"`
		DigestTime *string `json:"digest_time"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.productOwner.UpdateSettings(c.Request().Context(), ports.ProductOwnerSettingsUpdate{
		ProjectID: in.ProjectID, Enabled: in.Enabled, AgentID: in.AgentID, Prompt: in.Prompt, DigestTime: in.DigestTime,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}
