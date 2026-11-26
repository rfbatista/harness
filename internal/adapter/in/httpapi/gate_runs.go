package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The gate runs of a session (ADR 0012, 0014): the person asks for one from
// the agent box and reads what each recorded.

var errGateRunsOff = echo.NewHTTPError(http.StatusServiceUnavailable, "gate runs not configured")

// gateRunsView is a session's gate runs, oldest first.
type gateRunsView struct {
	Runs []domain.GateRun `json:"runs"`
}

func (h *Handler) handleRunSessionGates(c echo.Context) error {
	if h.gateRuns == nil {
		return errGateRunsOff
	}
	run, err := h.gateRuns.Run(c.Request().Context(), ports.GateRunRequest{SessionID: c.Param("id"), Trigger: domain.GateTriggerPerson})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, run)
}

func (h *Handler) handleListSessionGateRuns(c echo.Context) error {
	if h.gateRuns == nil {
		return errGateRunsOff
	}
	runs, err := h.gateRuns.List(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, gateRunsView{Runs: runs})
}
