package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Saved run commands and application runs. A run is a repository's app run
// from a session's worktree, in a terminal on the server.

func (h *Handler) handleListRunCommands(c echo.Context) error {
	if h.runCommands == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "run commands not configured")
	}
	list, err := h.runCommands.ListRunCommands(c.Request().Context(), c.QueryParam("repository_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"run_commands": list})
}

func (h *Handler) handleSaveRunCommand(c echo.Context) error {
	if h.runCommands == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "run commands not configured")
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Name         string `json:"name"`
		Command      string `json:"command"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cmd, err := h.runCommands.SaveRunCommand(c.Request().Context(), in.RepositoryID, in.Name, in.Command)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"run_command": cmd})
}

func (h *Handler) handleDeleteRunCommand(c echo.Context) error {
	if h.runCommands == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "run commands not configured")
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Name         string `json:"name"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.runCommands.DeleteRunCommand(c.Request().Context(), in.RepositoryID, in.Name); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleListRuns(c echo.Context) error {
	if h.apps == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "application runs not configured")
	}
	runs, err := h.apps.List(c.Request().Context(), c.QueryParam("session_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"runs": runs})
}

// handleStartRun runs a saved command (name) or a command line (command).
func (h *Handler) handleStartRun(c echo.Context) error {
	if h.apps == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "application runs not configured")
	}
	var in struct {
		SessionID string `json:"session_id"`
		Name      string `json:"name"`
		Command   string `json:"command"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	run, err := h.apps.Start(c.Request().Context(), in.SessionID, in.Name, in.Command)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"run": run})
}

func (h *Handler) handleStopRun(c echo.Context) error {
	if h.apps == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "application runs not configured")
	}
	var in struct {
		RunID string `json:"run_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	run, err := h.apps.Stop(c.Request().Context(), in.RunID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"run": run})
}
