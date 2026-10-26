package domain

import (
	"net/url"
	"strings"
	"time"
)

// Codes a pull request use case answers with. Their messages start with the
// code, so a client can show the message as it is.
const (
	CodePRNoRemote        = "PR_NO_REMOTE"
	CodePRNoCommits       = "PR_NO_COMMITS"
	CodePRBaseNotFound    = "PR_BASE_NOT_FOUND"
	CodePRAlreadyExists   = "PR_ALREADY_EXISTS"
	CodePRToolUnavailable = "PR_TOOL_UNAVAILABLE"
	CodePRPushFailed      = "PR_PUSH_FAILED"
	CodePRCreateFailed    = "PR_CREATE_FAILED"
	// CodePRFetchFailed: refreshing origin's branches failed for a reason other
	// than a credential the host refused.
	CodePRFetchFailed = "PR_FETCH_FAILED"
)

// PullRequestError is a coded pull request error whose message starts with
// its code.
func PullRequestError(code, message string) error {
	return &StructuredError{Code: code, Message: code + ": " + message}
}

// PullRequestState is where a pull request is on its host.
type PullRequestState string

const (
	PullRequestOpen   PullRequestState = "open"
	PullRequestClosed PullRequestState = "closed"
	PullRequestMerged PullRequestState = "merged"
)

// PullRequest is a pull request opened (or adopted) for a workspace's branch.
// It outlives the workspace: deleting the workspace row keeps it.
type PullRequest struct {
	WorkspaceID  string           `json:"workspace_id"`
	RepositoryID string           `json:"repository_id"`
	Number       int              `json:"number"`
	URL          string           `json:"url"`
	Title        string           `json:"title"`
	HeadBranch   string           `json:"head_branch"`
	BaseBranch   string           `json:"base_branch"`
	Draft        bool             `json:"draft"`
	State        PullRequestState `json:"state"`
	CreatedAt    time.Time        `json:"created_at"`
	// ProjectID routes the record's feed announcements; UpdatedAt is when it
	// was last stored. Neither is on the wire.
	ProjectID string    `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// Closed reports whether the pull request no longer stands open (closed or
// merged), so a new one may be opened for the workspace.
func (p PullRequest) Closed() bool { return p.State != PullRequestOpen }

// PullRequestDraft is what opening a pull request for a workspace would do
// now: suggested title and body, what it would compare, and the codes that
// would refuse it as far as the server knows locally.
type PullRequestDraft struct {
	WorkspaceID        string     `json:"workspace_id"`
	RepositoryID       string     `json:"repository_id"`
	HeadBranch         string     `json:"head_branch"`
	BaseBranch         string     `json:"base_branch"`
	Title              string     `json:"title"`
	Body               string     `json:"body"`
	Ahead              int        `json:"ahead"`
	UncommittedChanges int        `json:"uncommitted_changes"`
	Remote             *GitRemote `json:"remote"`
	Blockers           []string   `json:"blockers"`
}

// GitRemote is a remote repository on a code host, as parsed from its URL.
type GitRemote struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

// IsGitHub reports whether pull requests on the remote are opened on GitHub.
func (r GitRemote) IsGitHub() bool { return r.Host == "github.com" }

// ParseGitRemote reads host/owner/repo from a remote URL in scp form
// (git@host:owner/repo.git), ssh:// or http(s)://. Local paths, file:// and
// anything not exactly owner/repo are not code host remotes.
func ParseGitRemote(raw string) (GitRemote, bool) {
	raw = strings.TrimSpace(raw)
	var host, path string
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return GitRemote{}, false
		}
		switch u.Scheme {
		case "ssh", "git", "http", "https", "git+ssh":
		default:
			return GitRemote{}, false
		}
		host, path = u.Hostname(), u.Path
	default:
		// scp form: [user@]host:path, where host holds no slash.
		at := strings.LastIndex(raw, "@")
		colon := strings.Index(raw, ":")
		if colon < 0 || strings.Contains(raw[:colon], "/") || colon < at {
			return GitRemote{}, false
		}
		host, path = raw[at+1:colon], raw[colon+1:]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return GitRemote{}, false
	}
	return GitRemote{Host: strings.ToLower(host), Owner: parts[0], Repo: parts[1]}, true
}

// BranchFromRef is the branch name a ref names, without refs/heads/,
// refs/remotes/origin/ or origin/.
func BranchFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	for _, p := range []string{"refs/heads/", "refs/remotes/origin/", "origin/"} {
		if strings.HasPrefix(ref, p) {
			return strings.TrimPrefix(ref, p)
		}
	}
	return ref
}

// PullRequestBases is the branches a pull request of a workspace can be opened
// against: origin's, as last fetched.
type PullRequestBases struct {
	WorkspaceID string `json:"workspace_id"`
	// Branches are origin's branches without the origin/ prefix, HEAD
	// excluded, by name; empty when there is no origin or it was never fetched.
	Branches []string `json:"branches"`
	// Default is the draft's base: the branch the workspace was cut from.
	Default string `json:"default"`
	// FetchedAt is when origin was last fetched for the repository; nil when
	// it is not known.
	FetchedAt *time.Time `json:"fetched_at"`
}
