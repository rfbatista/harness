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
	repos, err := h.projects.ListRepositories(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repositories": repos})
}

func (h *Handler) handleGetRepository(c echo.Context) error {
	repo, err := h.projects.GetRepository(c.Request().Context(), c.QueryParam("repository_id"))
	if err != nil {
		return err
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
	repo, err := h.projects.CreateRepository(c.Request().Context(), in.ProjectID, in.Name, in.Description, in.URL, in.RootDir)
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
	repo, err := h.projects.UpdateRepository(c.Request().Context(), in.RepositoryID, in.Name, in.Description, in.URL, in.RootDir)
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
	if err := h.projects.DeleteRepository(c.Request().Context(), in.RepositoryID); err != nil {
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
	repo, err := h.projects.AddRepositoryIgnoredPath(c.Request().Context(), in.RepositoryID, in.Path)
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
	repo, err := h.projects.RemoveRepositoryIgnoredPath(c.Request().Context(), in.RepositoryID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repository": repo})
}
