package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// The orçamento de memória override of a repository or task: its own value
// in bytes (0 inherits) and what a session there resolves to. Both routes
// answer a domain.MemoryBudgetOverride.

var errMemoryBudgetsOff = echo.NewHTTPError(http.StatusServiceUnavailable, "memory budgets not configured")

func (h *Handler) handleGetMemoryBudgetOverride(c echo.Context) error {
	if h.memoryBudgets == nil {
		return errMemoryBudgetsOff
	}
	owner := domain.ActivationOwner{Level: domain.ActivationLevel(c.QueryParam("level")), ID: c.QueryParam("owner_id")}
	got, err := h.memoryBudgets.MemoryBudgetOverride(c.Request().Context(), owner)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, got)
}

func (h *Handler) handleSetMemoryBudgetOverride(c echo.Context) error {
	if h.memoryBudgets == nil {
		return errMemoryBudgetsOff
	}
	var in struct {
		Level   string `json:"level"`
		OwnerID string `json:"owner_id"`
		Bytes   int64  `json:"bytes"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	owner := domain.ActivationOwner{Level: domain.ActivationLevel(in.Level), ID: in.OwnerID}
	got, err := h.memoryBudgets.SetMemoryBudgetOverride(c.Request().Context(), owner, in.Bytes)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, got)
}
