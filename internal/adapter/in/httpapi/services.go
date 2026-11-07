package httpapi

import (
	"context"
	"net/http"
	"strconv"

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

// The person starts, stops and restarts services and reads their logs.

type serviceIDRequest struct {
	ServiceID string `json:"service_id"`
}

func (h *Handler) handleStartService(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	var in serviceIDRequest
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	svc, already, err := h.services.StartService(c.Request().Context(), in.ServiceID, personStart)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"service": svc, "already_running": already})
}

// personStart is a start by the person: always in the main checkout.
var personStart = ports.ServiceStartOptions{StartedBy: domain.ServiceStartedBy{Kind: "person"}}

func (h *Handler) handleStopService(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	var in serviceIDRequest
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	svc, err := h.services.StopService(c.Request().Context(), in.ServiceID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"service": svc})
}

func (h *Handler) handleRestartService(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	var in serviceIDRequest
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	svc, err := h.services.RestartService(c.Request().Context(), in.ServiceID, personStart)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"service": svc})
}

// optionalOffset reads a byte offset query parameter; absent is nil.
func optionalOffset(c echo.Context, name string) (*int64, error) {
	v := c.QueryParam(name)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: name + " must be a byte offset, zero or more"}
	}
	return &n, nil
}

func (h *Handler) handleReadServiceLog(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	offset, err := optionalOffset(c, "offset")
	if err != nil {
		return err
	}
	read, err := h.services.ReadServiceLog(c.Request().Context(), c.QueryParam("service_id"), offset)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, read)
}

// handleServiceLogEvents streams a service's log feed as bare `data:` lines
// of ports.ServiceLogEvent.
func (h *Handler) handleServiceLogEvents(c echo.Context) error {
	changes, err := h.followServiceLog(c.Request().Context(), c.QueryParam("service_id"), c.QueryParam("from"))
	if err != nil {
		return err
	}
	return h.streamSSE(c, changes)
}

func (h *Handler) followServiceLog(ctx context.Context, serviceID, from string) (<-chan any, error) {
	if h.services == nil {
		return nil, errServicesOff
	}
	var at *int64
	if from != "" {
		n, err := strconv.ParseInt(from, 10, 64)
		if err != nil || n < 0 {
			return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "from must be a byte offset, zero or more"}
		}
		at = &n
	}
	events, err := h.services.FollowServiceLog(ctx, serviceID, at)
	if err != nil {
		return nil, err
	}
	return relayAs(ctx, events, func(e ports.ServiceLogEvent) any { return e }), nil
}
