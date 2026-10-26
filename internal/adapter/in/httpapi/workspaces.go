package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

func (h *Handler) handleListWorkspaces(c echo.Context) error {
	repositoryID := c.QueryParam("repository_id")
	if repositoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repository_id is required")
	}
	return c.JSON(http.StatusOK, map[string]any{"workspaces": h.workspacesSvc.ListByRepository(repositoryID)})
}

func (h *Handler) handleCreateWorkspace(c echo.Context) error {
	var in struct {
		RepositoryID string `json:"repository_id"`
		Name         string `json:"name"`
		Branch       string `json:"branch"`
		BaseRef      string `json:"base_ref"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ws, err := h.workspacesSvc.Create(in.RepositoryID, in.Name, in.Branch, in.BaseRef)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"workspace": ws})
}

func (h *Handler) handleListBranches(c echo.Context) error {
	repositoryID := c.QueryParam("repository_id")
	if repositoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repository_id is required")
	}
	branches, err := h.workspacesSvc.ListBranches(repositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"branches": branches})
}

func (h *Handler) handleDeleteWorkspace(c echo.Context) error {
	var in struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.workspacesSvc.Delete(in.WorkspaceID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// handleCheckBranchName says whether a name is usable for a session's branch,
// and the name a start would generate. A bad or taken name is an answer, not
// an error: the start is the one that refuses.
func (h *Handler) handleCheckBranchName(c echo.Context) error {
	if h.branchNames == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "branch names are not configured")
	}
	repositoryID := c.QueryParam("repository_id")
	if repositoryID == "" {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "repository_id is required"}
	}
	check, err := h.branchNames.CheckBranchName(c.Request().Context(), repositoryID, c.QueryParam("name"), c.QueryParam("ticket_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, check)
}
