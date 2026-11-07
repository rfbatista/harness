package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A project's services (ADR 0076; see "Contract: Harness server ↔ Web UI —
// services").

var errServicesOff = echo.NewHTTPError(http.StatusServiceUnavailable, "services not configured")

func (h *Handler) handleListServices(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	list, err := h.services.ListServices(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, list)
}

func (h *Handler) handleGetService(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	svc, err := h.services.GetService(c.Request().Context(), c.QueryParam("service_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"service": svc})
}

func (h *Handler) handleCreateService(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	var in struct {
		ProjectID string              `json:"project_id"`
		Service   domain.ServiceInput `json:"service"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	svc, err := h.services.CreateService(c.Request().Context(), in.ProjectID, in.Service)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"service": svc})
}

func (h *Handler) handleUpdateService(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	var in struct {
		ServiceID string              `json:"service_id"`
		Service   domain.ServiceInput `json:"service"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	svc, restart, err := h.services.UpdateService(c.Request().Context(), in.ServiceID, in.Service)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"service": svc, "restart_needed": restart})
}

func (h *Handler) handleDeleteService(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	var in struct {
		ServiceID string `json:"service_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.services.DeleteService(c.Request().Context(), in.ServiceID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{})
}

// handleServiceEvents streams a project's services feed as bare `data:`
// lines of ports.ServiceEvent.
func (h *Handler) handleServiceEvents(c echo.Context) error {
	changes, err := h.followServices(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return h.streamSSE(c, changes)
}

func (h *Handler) followServices(ctx context.Context, projectID string) (<-chan any, error) {
	if h.services == nil {
		return nil, errServicesOff
	}
	changes, err := h.services.FollowServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return relayAs(ctx, changes, func(e ports.ServiceEvent) any { return e }), nil
}
