package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// handleClearArchivedWorktrees is the person's Clear worktrees action (ADR
// 0089): it removes the worktrees of one archived task, or of every archived
// task of a project, and answers what it cleared and what it skipped.
func (h *Handler) handleClearArchivedWorktrees(c echo.Context) error {
	if h.archivedTrees == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "clearing worktrees is not configured")
	}
	var in struct {
		TaskID    string `json:"task_id"`
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	res, err := h.archivedTrees.Clear(c.Request().Context(), ports.ArchivedWorktreesScope{TaskID: in.TaskID, ProjectID: in.ProjectID})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}
