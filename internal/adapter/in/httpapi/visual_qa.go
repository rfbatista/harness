package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

var errVisualQAOff = echo.NewHTTPError(http.StatusServiceUnavailable, "visual QA not configured")

// handleStartVisualQA starts visual QA for a session: a qa session on the
// repository's declared UI.
func (h *Handler) handleStartVisualQA(c echo.Context) error {
	if h.visualQA == nil {
		return errVisualQAOff
	}
	qa, err := h.visualQA.Start(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, qa)
}
