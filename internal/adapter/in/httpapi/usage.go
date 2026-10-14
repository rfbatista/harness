package httpapi

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Token usage as the LLM proxy measured it (see "Spec: harness — token usage
// monitoring and live dashboard").

var errUsageOff = echo.NewHTTPError(http.StatusServiceUnavailable, "usage not configured")

func (h *Handler) handleListUsageRecords(c echo.Context) error {
	if h.usage == nil {
		return errUsageOff
	}
	q := ports.UsageQuery{ProjectID: c.Param("project")}
	if s := c.QueryParam("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "limit must be a number from 1 to 200"}
		}
		if n == 0 {
			n = -1 // an explicit 0 is out of range, not the default
		}
		q.Limit = n
	}
	page, err := h.usage.ListRecentUsage(c.Request().Context(), q)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
}
