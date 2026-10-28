package httpclient

import (
	"context"
	"net/url"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.WorktreeGit = (*WorktreeGit)(nil)

// WorktreeGit reads and acts on worktree git state over /api.
type WorktreeGit struct{ c *Client }

// NewWorktreeGit returns the worktree git adapter over c.
func NewWorktreeGit(c *Client) *WorktreeGit { return &WorktreeGit{c: c} }

func (w *WorktreeGit) GitState(ctx context.Context, workspaceID string) (*domain.GitState, error) {
	v := url.Values{}
	set(v, "workspace_id", workspaceID)
	var out struct {
		State *domain.GitState `json:"state"`
	}
	if err := w.c.get(ctx, "/api/worktree_git_state", v, &out); err != nil {
		return nil, err
	}
	return out.State, nil
}

func (w *WorktreeGit) TaskGitStates(ctx context.Context, taskID string) ([]domain.GitState, error) {
	v := url.Values{}
	set(v, "task_id", taskID)
	var out struct {
		States []domain.GitState `json:"states"`
	}
	if err := w.c.get(ctx, "/api/list_worktree_git_states", v, &out); err != nil {
		return nil, err
	}
	return out.States, nil
}

func (w *WorktreeGit) FetchRemote(ctx context.Context, repositoryID string) (time.Time, error) {
	var out struct {
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := w.c.post(ctx, "/api/fetch_remote", map[string]string{"repository_id": repositoryID}, &out); err != nil {
		return time.Time{}, err
	}
	return out.FetchedAt, nil
}
