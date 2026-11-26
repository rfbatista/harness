package httpapi

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// The eval bench (ADR 0015): a project's bench cases.

var errBenchOff = echo.NewHTTPError(http.StatusServiceUnavailable, "eval bench not configured")

func (h *Handler) handleListBenchCases(c echo.Context) error {
	if h.bench == nil {
		return errBenchOff
	}
	cases, err := h.bench.ListCases(c.Request().Context(), c.Param("project"))
	if err != nil {
		return err
	}
	if cases == nil {
		cases = []domain.BenchCase{}
	}
	return c.JSON(http.StatusOK, map[string]any{"cases": cases})
}

func (h *Handler) handleCreateBenchCase(c echo.Context) error {
	if h.bench == nil {
		return errBenchOff
	}
	var def domain.BenchCase
	if err := bindJSON(c, &def); err != nil {
		return err
	}
	created, err := h.bench.CreateCase(c.Request().Context(), c.Param("project"), def)
	var ve *domain.BenchCaseValidationError
	if errors.As(err, &ve) {
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"errors": ve.Errors})
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, created)
}

func (h *Handler) handleDeleteBenchCase(c echo.Context) error {
	if h.bench == nil {
		return errBenchOff
	}
	if err := h.bench.DeleteCase(c.Request().Context(), c.Param("project"), c.Param("case")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
