package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// handlePullRequest answers a workspace's pull request page: the draft and
// the newest PR stored for it, from local state only.
func (h *Handler) handlePullRequest(c echo.Context) error {
	if h.pullRequests == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "pull requests not configured")
	}
	workspaceID := c.QueryParam("workspace_id")
	if workspaceID == "" {
		return domain.PullRequestError("INVALID_INPUT", "workspace_id is required")
	}
	view, err := h.pullRequests.PullRequest(c.Request().Context(), workspaceID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

func (h *Handler) handleCreatePullRequest(c echo.Context) error {
	if h.pullRequests == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "pull requests not configured")
	}
	var in struct {
		WorkspaceID string `json:"workspace_id"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		BaseBranch  string `json:"base_branch"`
		Draft       bool   `json:"draft"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	pr, err := h.pullRequests.CreatePullRequest(c.Request().Context(), ports.CreatePullRequest{
		WorkspaceID: in.WorkspaceID, Title: in.Title, Body: in.Body, BaseBranch: in.BaseBranch, Draft: in.Draft,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"pull_request": pr})
}

// handlePullRequestBases lists origin's branches as last fetched, from local
// git only, for the base picker of a workspace's pull request.
func (h *Handler) handlePullRequestBases(c echo.Context) error {
	if h.prBases == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "pull requests not configured")
	}
	workspaceID := c.QueryParam("workspace_id")
	if workspaceID == "" {
		return domain.PullRequestError("INVALID_INPUT", "workspace_id is required")
	}
	bases, err := h.prBases.PullRequestBases(c.Request().Context(), workspaceID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, bases)
}

// handleRefreshPullRequestBases fetches origin with the stored credentials and
// answers the new list.
func (h *Handler) handleRefreshPullRequestBases(c echo.Context) error {
	if h.prBases == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "pull requests not configured")
	}
	var in struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.WorkspaceID == "" {
		return domain.PullRequestError("INVALID_INPUT", "workspace_id is required")
	}
	bases, err := h.prBases.RefreshPullRequestBases(c.Request().Context(), in.WorkspaceID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, bases)
}
