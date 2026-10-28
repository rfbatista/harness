package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// The git state of a session worktree. Every answer and error code comes from
// ports.WorktreeGit; the handlers only read the request.

func (h *Handler) worktreeGitOr503() error {
	if h.worktreeGit == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "worktree git state is not available"}
	}
	return nil
}

func (h *Handler) handleWorktreeGitState(c echo.Context) error {
	if err := h.worktreeGitOr503(); err != nil {
		return err
	}
	st, err := h.worktreeGit.GitState(c.Request().Context(), c.QueryParam("workspace_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"state": st})
}

func (h *Handler) handleListWorktreeGitStates(c echo.Context) error {
	if err := h.worktreeGitOr503(); err != nil {
		return err
	}
	states, err := h.worktreeGit.TaskGitStates(c.Request().Context(), c.QueryParam("task_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"states": states})
}

func (h *Handler) handleFetchRemote(c echo.Context) error {
	if err := h.worktreeGitOr503(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	at, err := h.worktreeGit.FetchRemote(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"fetched_at": at})
}
