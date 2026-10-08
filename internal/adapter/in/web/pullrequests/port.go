// Package pullrequests is the pages' side of a workspace's pull request: the
// history page's toolbar control and the task page's badges, rendered for the
// first paint and handed to the browser's pullrequests module with a seed in
// the Web UI contract's JSON shape.
package pullrequests

import (
	"context"
	"time"
)

// Reader is what the pages read of pull requests: the read side of
// ports.PullRequests. Neither method waits on the host.
//
// The types below mirror domain.PullRequest, domain.PullRequestDraft and
// ports.PullRequestView field for field (same JSON) until the server's
// branch lands; they then become aliases of those.
type Reader interface {
	// PullRequest is the draft and the newest PR stored for the workspace,
	// from git and the store; WORKSPACE_NOT_FOUND once the workspace is gone.
	PullRequest(ctx context.Context, workspaceID string) (*View, error)
	// StoredPullRequests is the newest stored PR per workspace that has one:
	// the store only, no git, no host.
	StoredPullRequests(ctx context.Context, workspaceIDs []string) ([]*PullRequest, error)
}

// PullRequest is a pull request opened for a workspace's branch.
type PullRequest struct {
	WorkspaceID  string    `json:"workspace_id"`
	RepositoryID string    `json:"repository_id"`
	Number       int       `json:"number"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	HeadBranch   string    `json:"head_branch"`
	BaseBranch   string    `json:"base_branch"`
	Draft        bool      `json:"draft"`
	State        string    `json:"state"`
	CreatedAt    time.Time `json:"created_at"`
}

// Remote is the host repository origin points at.
type Remote struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

// Draft is what opening a pull request would do now.
type Draft struct {
	WorkspaceID        string   `json:"workspace_id"`
	RepositoryID       string   `json:"repository_id"`
	HeadBranch         string   `json:"head_branch"`
	BaseBranch         string   `json:"base_branch"`
	Title              string   `json:"title"`
	Body               string   `json:"body"`
	Ahead              int      `json:"ahead"`
	UncommittedChanges int      `json:"uncommitted_changes"`
	Remote             *Remote  `json:"remote"`
	Blockers           []string `json:"blockers"`
}

// View is a workspace's draft and its newest stored PR (nil when none).
type View struct {
	Draft       Draft        `json:"draft"`
	PullRequest *PullRequest `json:"pull_request"`
}
