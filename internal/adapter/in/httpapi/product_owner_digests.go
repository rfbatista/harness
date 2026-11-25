package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// handleListProductOwnerDigests answers a project's digests, newest first, 30
// at most: the product owner's daily record (ADR 0113).
func (h *Handler) handleListProductOwnerDigests(c echo.Context) error {
	if err := h.productOwnerReady(); err != nil {
		return err
	}
	digests, ok := h.productOwner.(ports.ProductOwnerDigests)
	if !ok {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "digests are not available on this server"}
	}
	list, more, err := digests.Digests(c.Request().Context(), c.QueryParam("project_id"), c.QueryParam("before"))
	if err != nil {
		return err
	}
	if list == nil {
		list = []domain.ProductOwnerDigest{}
	}
	return c.JSON(http.StatusOK, map[string]any{"digests": list, "has_earlier": more})
}
