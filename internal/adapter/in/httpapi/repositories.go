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

// handleFindRepositories lists the git checkouts inside root_dir on the
// server's machine, for a client choosing which to add:
// {"root": "<resolved dir>", "repositories": [{"path", "name", "remote"}]}.
func (h *Handler) handleFindRepositories(c echo.Context) error {
	if h.discovery == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "repository discovery not configured")
	}
	root, found, err := h.discovery.FindRepositories(c.Request().Context(), c.QueryParam("root_dir"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"root": root, "repositories": found})
}

// Env files: what the harness writes into a repository's session worktrees.

func (h *Handler) envOr503() error {
	if h.env == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "env files not configured")
	}
	return nil
}

func (h *Handler) handleListEnvFiles(c echo.Context) error {
	if err := h.envOr503(); err != nil {
		return err
	}
	files, err := h.env.ListEnvFiles(c.Request().Context(), c.QueryParam("repository_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"env_files": files})
}

func (h *Handler) handleSaveEnvFile(c echo.Context) error {
	if err := h.envOr503(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
		Content      string `json:"content"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	f, err := h.env.SaveEnvFile(c.Request().Context(), in.RepositoryID, in.Path, in.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"env_file": f})
}

func (h *Handler) handleDeleteEnvFile(c echo.Context) error {
	if err := h.envOr503(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.env.DeleteEnvFile(c.Request().Context(), in.RepositoryID, in.Path); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleImportEnvFile(c echo.Context) error {
	if err := h.envOr503(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	f, err := h.env.ImportEnvFile(c.Request().Context(), in.RepositoryID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"env_file": f})
}
