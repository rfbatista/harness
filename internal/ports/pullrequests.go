package ports

import (
	"context"
	"errors"
	"time"

	"operators-mcp/internal/domain"
)

// CreatePullRequest is what a person asks for when opening a pull request for
// a workspace. BaseBranch "" means the draft's base.
type CreatePullRequest struct {
	WorkspaceID string
	Title       string
	Body        string
	BaseBranch  string
	Draft       bool
}

// PullRequestView is a workspace's pull request page: what opening one would
// do now, and the newest one stored for it (nil when none).
type PullRequestView struct {
	Draft       domain.PullRequestDraft `json:"draft"`
	PullRequest *domain.PullRequest     `json:"pull_request"`
}

// PullRequests opens a workspace's branch as a pull request on its origin
// (driving). *workspaces.PullRequests satisfies it.
type PullRequests interface {
	// PullRequest answers from git and the stored record only, never waiting
	// on the host; an unknown workspace is WORKSPACE_NOT_FOUND.
	PullRequest(ctx context.Context, workspaceID string) (*PullRequestView, error)
	// CreatePullRequest pushes the workspace branch to origin and opens the
	// pull request, answering the PR_* codes when it cannot.
	CreatePullRequest(ctx context.Context, req CreatePullRequest) (*domain.PullRequest, error)
}

// ErrRemoteBranchNotFound is BranchPublisher.FetchBranch's answer for a
// branch origin does not have.
var ErrRemoteBranchNotFound = errors.New("branch not found on origin")

// BranchPublisher reads a checkout's origin and publishes its branches there
// (driven; gitcli implements it). It never force-pushes and never touches a
// working tree: only refs and the branch's upstream config change.
type BranchPublisher interface {
	// OriginURL is the origin remote's URL, "" when there is no origin.
	OriginURL(ctx context.Context, repoRoot string) (string, error)
	// HasRef reports whether ref (e.g. refs/remotes/origin/main) resolves.
	HasRef(ctx context.Context, repoRoot, ref string) bool
	// FetchBranch updates refs/remotes/origin/<branch> from origin, or answers
	// ErrRemoteBranchNotFound.
	FetchBranch(ctx context.Context, repoRoot, branch string) error
	// Push publishes the local branch to origin under the same name and sets
	// its upstream. A rejection carries git's reason.
	Push(ctx context.Context, repoRoot, branch string) error
	// Subjects are the subjects of the commits head has that base lacks,
	// newest first, at most limit.
	Subjects(ctx context.Context, repoRoot, base, head string, limit int) ([]string, error)
}

// HostPullRequest is a pull request as its code host reports it.
type HostPullRequest struct {
	Number    int
	URL       string
	Title     string
	Head      string
	Base      string
	Draft     bool
	State     domain.PullRequestState
	CreatedAt time.Time
}

// OpenPullRequest is what CodeHost.Open sends.
type OpenPullRequest struct {
	Head  string
	Base  string
	Title string
	Body  string
	Draft bool
}

// ErrPullRequestExists is CodeHost.Open's answer when the host already has an
// open pull request for the head.
var ErrPullRequestExists = errors.New("a pull request already exists for this head")

// CodeHost opens and reads pull requests as the person signed in on this
// machine (driven; ghcli implements it).
type CodeHost interface {
	// Ready is PR_TOOL_UNAVAILABLE, saying what to run, when the host cannot
	// be reached as the person. It does not go over the network.
	Ready(ctx context.Context) error
	// FindOpen is the open pull request for head, nil when there is none.
	FindOpen(ctx context.Context, r domain.GitRemote, head string) (*HostPullRequest, error)
	// Open opens a pull request: PR_CREATE_FAILED with the host's reason, or
	// ErrPullRequestExists.
	Open(ctx context.Context, r domain.GitRemote, req OpenPullRequest) (*HostPullRequest, error)
	Get(ctx context.Context, r domain.GitRemote, number int) (*HostPullRequest, error)
}

// PullRequestRepository keeps the pull requests opened for workspaces. They
// outlive the workspace row and go with their project.
type PullRequestRepository interface {
	// Save inserts or updates the record of (RepositoryID, Number).
	Save(pr *domain.PullRequest) (*domain.PullRequest, error)
	// LatestForWorkspace is the workspace's newest pull request, any state;
	// nil when none.
	LatestForWorkspace(workspaceID string) *domain.PullRequest
	DeleteByProject(projectID string) error
}
