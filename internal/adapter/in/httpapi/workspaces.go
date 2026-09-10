package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
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
