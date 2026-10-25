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

// catalogWrite binds a write's body, runs it and answers its result.
func catalogWrite[T any](h *Handler, c echo.Context, run func(T) (*domain.CatalogWriteResult, error)) error {
	if err := h.catalogOrUnavailable(); err != nil {
		return err
	}
	var in T
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	out, err := run(in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) handleSaveCatalogAgent(c echo.Context) error {
	return catalogWrite(h, c, func(in domain.SaveCatalogAgent) (*domain.CatalogWriteResult, error) {
		return h.catalog.SaveCatalogAgent(in)
	})
}

func (h *Handler) handleDeleteCatalogAgent(c echo.Context) error {
	return catalogWrite(h, c, func(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error) {
		return h.catalog.DeleteCatalogAgent(in)
	})
}

func (h *Handler) handleSaveCatalogBundle(c echo.Context) error {
	return catalogWrite(h, c, func(in domain.SaveCatalogBundle) (*domain.CatalogWriteResult, error) {
		return h.catalog.SaveCatalogBundle(in)
	})
}

func (h *Handler) handleDeleteCatalogBundle(c echo.Context) error {
	return catalogWrite(h, c, func(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error) {
		return h.catalog.DeleteCatalogBundle(in)
	})
}

func (h *Handler) handleUndoCatalogChange(c echo.Context) error {
	return catalogWrite(h, c, func(in domain.UndoCatalogChange) (*domain.CatalogWriteResult, error) {
		return h.catalog.UndoCatalogChange(in)
	})
}

func (h *Handler) handleSaveCatalogSkill(c echo.Context) error {
	return catalogWrite(h, c, func(in domain.SaveCatalogSkill) (*domain.CatalogWriteResult, error) {
		return h.catalog.SaveCatalogSkill(in)
	})
}

func (h *Handler) handleDeleteCatalogSkill(c echo.Context) error {
	return catalogWrite(h, c, func(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error) {
		return h.catalog.DeleteCatalogSkill(in)
	})
}
