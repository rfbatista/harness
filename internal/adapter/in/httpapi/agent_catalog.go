package httpapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// catalogOrUnavailable is the agent catalog, or an UNAVAILABLE error when
// this server was built without one.
func (h *Handler) catalogOrUnavailable() error {
	if h.catalog == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "the agent catalog is not available"}
	}
	return nil
}

func (h *Handler) handleGetAgentCatalog(c echo.Context) error {
	if err := h.catalogOrUnavailable(); err != nil {
		return err
	}
	view, err := h.catalog.ReadCatalog()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

func (h *Handler) handleGetCatalogSkill(c echo.Context) error {
	if err := h.catalogOrUnavailable(); err != nil {
		return err
	}
	view, err := h.catalog.ReadCatalogSkill(c.QueryParam("name"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

func (h *Handler) handleCheckEnvVars(c echo.Context) error {
	if err := h.catalogOrUnavailable(); err != nil {
		return err
	}
	var names []string
	for _, n := range strings.Split(c.QueryParam("names"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return c.JSON(http.StatusOK, h.catalog.CheckEnvVars(names))
}
