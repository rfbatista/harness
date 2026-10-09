package pullrequests

import (
	"context"
	"strconv"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Panel is the history page's control for one workspace: what it paints
// before the browser takes over, and the seed it takes over from.
type Panel struct {
	Seed PanelSeed
	// Chip is the newest PR; nil when there is none.
	Chip *Chip
	// OffersCreate: no PR is open, so the create button shows.
	OffersCreate bool
	// Blockers are why a create cannot go ahead, in words; empty when it can.
	Blockers []string
}

// PanelSeed is the panel's seed: GET /api/pull_request's answer with the
// workspace and its project. PullRequestView is nil when the server could not read it,
// and the browser then reads it itself.
type PanelSeed struct {
	ProjectID   string `json:"project_id"`
	WorkspaceID string `json:"workspace_id"`
	*ports.PullRequestView
}

// Chip is a PR as its chip and badges show it.
type Chip struct {
	Label, Short, Title, Href, State, Tone, Describe string
}

// NewPanel is the panel for a workspace; view nil paints nothing yet.
func NewPanel(projectID, workspaceID string, view *ports.PullRequestView) *Panel {
	p := &Panel{Seed: PanelSeed{ProjectID: projectID, WorkspaceID: workspaceID, PullRequestView: view}}
	if view == nil {
		return p
	}
	if view.PullRequest != nil {
		p.Chip = NewChip(view.PullRequest)
	}
	p.OffersCreate = view.PullRequest == nil || view.PullRequest.Closed()
	if p.OffersCreate {
		for _, code := range view.Draft.Blockers {
			p.Blockers = append(p.Blockers, BlockerText(code, view.Draft))
		}
	}
	return p
}

// Blocked reports whether the create button is disabled.
func (p *Panel) Blocked() bool { return len(p.Blockers) > 0 }

// Painted reports whether the server painted the control (it had a view).
func (p *Panel) Painted() bool { return p.Seed.PullRequestView != nil }

var tones = map[string]string{"open": "success", "draft": "", "merged": "signal", "closed": "danger"}

// ShownState is "draft" for an open PR marked draft, else its state.
func ShownState(pr *domain.PullRequest) string {
	if pr.State == domain.PullRequestOpen && pr.Draft {
		return "draft"
	}
	return string(pr.State)
}

// NewChip is how a PR reads on its chip and badges.
func NewChip(pr *domain.PullRequest) *Chip {
	state := ShownState(pr)
	n := strconv.Itoa(pr.Number)
	return &Chip{
		Label: "#" + n, Short: "PR #" + n, Title: pr.Title, Href: pr.URL, State: state, Tone: tones[state],
		Describe: "Pull request #" + n + ", " + state + ": " + pr.Title + ". Opens on GitHub.",
	}
}

// BlockerText is why a create cannot go ahead, for one blocker code. The
// browser's presentation/view.js says the same; web/testdata/views/pull-request.json pins both.
func BlockerText(code string, d domain.PullRequestDraft) string {
	switch code {
	case "PR_NO_COMMITS":
		return "Nothing to propose yet: " + d.HeadBranch + " has no commits ahead of " + d.BaseBranch + "."
	case "PR_NO_REMOTE":
		return "This repository has no GitHub origin remote, so there is nowhere to open a pull request."
	case "PR_TOOL_UNAVAILABLE":
		return "The server can't reach GitHub as you: gh is missing or not signed in on the server machine. Run gh auth login there."
	case "PR_BASE_NOT_FOUND":
		return d.BaseBranch + " is not a branch on origin."
	case "PR_ALREADY_EXISTS":
		return "A pull request is already open for this branch."
	default:
		return "The server would refuse it (" + code + ")."
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// Summary is "3 commits from agent/x into main on acme/harness".
func Summary(d domain.PullRequestDraft) string {
	where := ""
	if d.Remote != nil {
		where = " on " + d.Remote.Owner + "/" + d.Remote.Repo
	}
	return plural(d.Ahead, "commit", "commits") + " from " + d.HeadBranch + " into " + d.BaseBranch + where
}

// Uncommitted warns of what the PR will leave out; "" when nothing.
func Uncommitted(d domain.PullRequestDraft) string {
	if d.UncommittedChanges == 0 {
		return ""
	}
	return plural(d.UncommittedChanges, "uncommitted change", "uncommitted changes") + " in the worktree will not be in the pull request."
}

// WatchSeed is the task page's: its sessions' workspaces and the newest PR
// stored for each that has one, for the rows' badges.
type WatchSeed struct {
	ProjectID    string                `json:"project_id"`
	WorkspaceIDs []string              `json:"workspace_ids"`
	PullRequests []*domain.PullRequest `json:"pull_requests"`
}

// NewWatchSeed reads the stored PRs of the workspaces (never the host). A
// failed read seeds none: the badges then fill from the feed. Neither list
// is ever null.
func NewWatchSeed(ctx context.Context, r ports.PullRequests, projectID string, workspaceIDs []string) *WatchSeed {
	s := &WatchSeed{ProjectID: projectID, WorkspaceIDs: []string{}, PullRequests: []*domain.PullRequest{}}
	s.WorkspaceIDs = append(s.WorkspaceIDs, workspaceIDs...)
	if len(workspaceIDs) == 0 {
		return s
	}
	if prs, err := r.StoredPullRequests(ctx, workspaceIDs); err == nil {
		for _, pr := range prs {
			if pr != nil {
				s.PullRequests = append(s.PullRequests, pr)
			}
		}
	}
	return s
}
