package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// The container runtime image services run under (contract section 9).

func runtimeAnswer(info ports.ServiceRuntimeInfo) map[string]any {
	return map[string]any{"container_runtime": info.Name, "available": info.Available}
}

func (h *Handler) handleGetServicesSettings(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	return c.JSON(http.StatusOK, runtimeAnswer(h.services.ContainerRuntime(c.Request().Context())))
}

func (h *Handler) handleSetServicesSettings(c echo.Context) error {
	if h.services == nil {
		return errServicesOff
	}
	var in struct {
		ContainerRuntime string `json:"container_runtime"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	info, err := h.services.SetContainerRuntime(c.Request().Context(), in.ContainerRuntime)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, runtimeAnswer(info))
}
