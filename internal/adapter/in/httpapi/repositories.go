package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) handleListRepositories(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	return c.JSON(http.StatusOK, map[string]any{"repositories": h.svc.ListRepositories(projectID)})
}

func (h *Handler) handleGetRepository(c echo.Context) error {
	repo := h.svc.GetRepository(c.QueryParam("repository_id"))
	if repo == nil {
		return echo.NewHTTPError(http.StatusNotFound, "repository not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"repository": repo})
}

func (h *Handler) handleCreateRepository(c echo.Context) error {
	var in struct {
		ProjectID   string `json:"project_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		URL         string `json:"url"`
		RootDir     string `json:"root_dir"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	repo, err := h.svc.CreateRepository(in.ProjectID, in.Name, in.Description, in.URL, in.RootDir)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repository": repo})
}

func (h *Handler) handleUpdateRepository(c echo.Context) error {
	var in struct {
		RepositoryID string `json:"repository_id"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		URL          string `json:"url"`
		RootDir      string `json:"root_dir"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	repo, err := h.svc.UpdateRepository(in.RepositoryID, in.Name, in.Description, in.URL, in.RootDir)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repository": repo})
}

func (h *Handler) handleDeleteRepository(c echo.Context) error {
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.svc.DeleteRepository(in.RepositoryID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleAddRepositoryIgnoredPath(c echo.Context) error {
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	repo, err := h.svc.AddRepositoryIgnoredPath(in.RepositoryID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repository": repo})
}

func (h *Handler) handleRemoveRepositoryIgnoredPath(c echo.Context) error {
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	repo, err := h.svc.RemoveRepositoryIgnoredPath(in.RepositoryID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repository": repo})
}
