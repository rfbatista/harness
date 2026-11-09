package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
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

func (h *Handler) handlePushBranch(c echo.Context) error {
	if err := h.worktreeGitOr503(); err != nil {
		return err
	}
	var in struct {
		WorkspaceID  string `json:"workspace_id"`
		ExpectedHead string `json:"expected_head"`
		// Additive (web contract §2): all three default as the person's Push did.
		Branch       string `json:"branch"`
		Remote       string `json:"remote"`
		RemoteBranch string `json:"remote_branch"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	st, err := h.worktreeGit.PushBranchTo(c.Request().Context(), ports.PushTarget{
		WorkspaceID: in.WorkspaceID, ExpectedHead: in.ExpectedHead, Branch: in.Branch, Remote: in.Remote, RemoteBranch: in.RemoteBranch,
	})
	if err != nil {
		return err
	}
	out := map[string]any{"state": st}
	// The pushed branch's row, now showing where it went (web contract §2).
	if h.sessionBranches != nil {
		name := in.Branch
		if name == "" {
			name = st.Branch
		}
		if rows, err := h.sessionBranches.SessionBranches(c.Request().Context(), st.SessionID); err == nil {
			for i := range rows {
				if rows[i].Name == name {
					out["branch"] = rows[i]
				}
			}
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) handleRepositoryRemotes(c echo.Context) error {
	if err := h.worktreeGitOr503(); err != nil {
		return err
	}
	lists, ok := h.worktreeGit.(ports.RepositoryRemoteLists)
	if !ok {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "repository remotes are not available"}
	}
	got, err := lists.RepositoryRemotes(c.Request().Context(), c.QueryParam("repository_id"))
	if err != nil {
		return err
	}
	remotes := make([]map[string]string, 0, len(got))
	for _, r := range got {
		remotes = append(remotes, map[string]string{"name": r.Name, "url": r.URL})
	}
	return c.JSON(http.StatusOK, map[string]any{"remotes": remotes})
}
