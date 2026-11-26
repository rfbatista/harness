package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) handleGetContextMap(c echo.Context) error {
	if h.contextMaps == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "context maps are not configured")
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.contextMaps.GetContextMap(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}
