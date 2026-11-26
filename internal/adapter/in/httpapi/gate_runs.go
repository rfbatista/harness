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

// handleGateOutput streams one gate's output as plain text: what a running gate
// has written so far, then the rest as it comes, ending when the gate does. A
// finished run answers what it recorded.
func (h *Handler) handleGateOutput(c echo.Context) error {
	if h.gateRuns == nil {
		return errGateRunsOff
	}
	w := &flushWriter{c: c}
	err := h.gateRuns.Output(c.Request().Context(), c.Param("id"), c.Param("run"), domain.GateKind(c.Param("kind")), w)
	if err != nil && !w.started {
		return err
	}
	if !w.started {
		c.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")
		c.Response().WriteHeader(http.StatusOK)
	}
	return nil
}

// flushWriter sends each write to the client at once, starting the response
// with the first.
type flushWriter struct {
	c       echo.Context
	started bool
}

func (f *flushWriter) Write(p []byte) (int, error) {
	res := f.c.Response()
	if !f.started {
		f.started = true
		res.Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")
		res.Header().Set("Cache-Control", "no-store")
		res.WriteHeader(http.StatusOK)
	}
	n, err := res.Write(p)
	res.Flush()
	return n, err
}
