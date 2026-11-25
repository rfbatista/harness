package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

var errTopBarOff = echo.NewHTTPError(http.StatusServiceUnavailable, "top bar not configured")

// handleTopBar answers the top bar's model for ?path=, a page's URL path (see
// "Contract: Harness server ↔ Web UI — the top bar island").
func (h *Handler) handleTopBar(c echo.Context) error {
	if h.topBars == nil {
		return errTopBarOff
	}
	path := c.QueryParam("path")
	if !isPagePath(path) {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "path is required and must be a URL path such as /projects/p1"}
	}
	model, err := h.topBars.TopBar(c.Request().Context(), path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, model)
}

// isPagePath says whether s is a URL path (what location.pathname holds): it
// starts with one slash, and carries no host, query, fragment or control
// character.
func isPagePath(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.ContainsAny(s, "?#\\") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "" && u.Host == ""
}
