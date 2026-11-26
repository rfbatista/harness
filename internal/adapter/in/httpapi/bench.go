package httpapi

import (
	"encoding/json"
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

func (h *Handler) handleStartBenchRun(c echo.Context) error {
	if h.benchRuns == nil {
		return errBenchOff
	}
	var req domain.BenchRunRequest
	// The body is optional: no body runs on the defaults.
	if err := bindJSON(c, &req); err != nil && c.Request().ContentLength != 0 {
		return err
	}
	run, err := h.benchRuns.StartRun(c.Request().Context(), c.Param("project"), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, run)
}

func (h *Handler) handleGetBenchRun(c echo.Context) error {
	if h.benchRuns == nil {
		return errBenchOff
	}
	run, err := h.benchRuns.GetRun(c.Request().Context(), c.Param("project"), c.Param("run"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, run)
}

func (h *Handler) handleCancelBenchRun(c echo.Context) error {
	if h.benchRuns == nil {
		return errBenchOff
	}
	run, err := h.benchRuns.CancelRun(c.Request().Context(), c.Param("project"), c.Param("run"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, run)
}

func (h *Handler) handleListBenchScorecards(c echo.Context) error {
	if h.benchRuns == nil {
		return errBenchOff
	}
	cards, err := h.benchRuns.ListScorecards(c.Request().Context(), c.Param("project"))
	if err != nil {
		return err
	}
	if cards == nil {
		cards = []domain.Scorecard{}
	}
	return c.JSON(http.StatusOK, map[string]any{"scorecards": cards})
}

func (h *Handler) handleGetBenchScorecard(c echo.Context) error {
	if h.benchRuns == nil {
		return errBenchOff
	}
	card, err := h.benchRuns.GetScorecard(c.Request().Context(), c.Param("project"), c.Param("scorecard"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, card)
}

func (h *Handler) handleCompareBenchScorecard(c echo.Context) error {
	if h.benchRuns == nil {
		return errBenchOff
	}
	cmp, err := h.benchRuns.CompareScorecard(c.Request().Context(), c.Param("project"), c.Param("scorecard"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, cmp)
}

func (h *Handler) handleCreateBenchCaseFromTrajectory(c echo.Context) error {
	if h.benchTrajectories == nil {
		return errBenchOff
	}
	var body struct {
		TrajectoryID string `json:"trajectory_id"`
	}
	if err := bindJSON(c, &body); err != nil {
		return err
	}
	created, err := h.benchTrajectories.CreateCaseFromTrajectory(c.Request().Context(), c.Param("project"), body.TrajectoryID)
	var ve *domain.BenchCaseValidationError
	if errors.As(err, &ve) {
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"errors": ve.Errors})
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, created)
}

func (h *Handler) handleListBenchTrajectories(c echo.Context) error {
	if h.benchTrajectories == nil {
		return errBenchOff
	}
	list, err := h.benchTrajectories.ListTrajectories(c.Request().Context(), c.Param("project"))
	if err != nil {
		return err
	}
	if list == nil {
		list = []domain.BenchTrajectory{}
	}
	return c.JSON(http.StatusOK, map[string]any{"trajectories": list})
}

func (h *Handler) handleGetBenchTrajectory(c echo.Context) error {
	if h.benchTrajectories == nil {
		return errBenchOff
	}
	t, err := h.benchTrajectories.GetTrajectory(c.Request().Context(), c.Param("project"), c.Param("trajectory"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, t)
}

// handleGetTaskTrajectoryRecording answers whether a task's sessions are
// recorded; ?repository= asks as that repository's sessions would.
func (h *Handler) handleGetTaskTrajectoryRecording(c echo.Context) error {
	if h.benchTrajectories == nil {
		return errBenchOff
	}
	rec, err := h.benchTrajectories.TaskRecording(c.Request().Context(), c.Param("project"), c.Param("task"), c.QueryParam("repository"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, rec)
}

// handleSetTaskTrajectoryRecording opts a task in or out: {"enabled": true},
// {"enabled": false}, or {"enabled": null} to follow the settings default.
func (h *Handler) handleSetTaskTrajectoryRecording(c echo.Context) error {
	if h.benchTrajectories == nil {
		return errBenchOff
	}
	var body map[string]json.RawMessage
	if err := bindJSON(c, &body); err != nil {
		return err
	}
	// An empty body or a key the flag does not have is refused, not read as
	// "clear the flag"; only an explicit null does that.
	raw, ok := body["enabled"]
	if !ok || len(body) != 1 {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, `the body is {"enabled": true|false|null} and nothing else`)
	}
	var enabled *bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	rec, err := h.benchTrajectories.SetTaskRecording(c.Request().Context(), c.Param("project"), c.Param("task"), enabled)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, rec)
}
