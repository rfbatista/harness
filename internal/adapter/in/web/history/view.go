package history

import (
	"time"

	"operators-mcp/internal/adapter/in/web/code"
	"operators-mcp/internal/adapter/in/web/gitsync"
	"operators-mcp/internal/adapter/in/web/pullrequests"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// PageView is a history page: a repository's, or one session's branch.
type PageView struct {
	Frame   shell.Frame
	Crumbs  []Crumb
	Heading string
	// Meta is the line beside the heading: how many commits, or where a
	// session's branch stands against its base.
	Meta string
	// RemotesHref toggles remote-tracking branches (the repository page);
	// "" hides the toggle.
	RemotesHref string
	Remotes     bool
	// Notice explains something about what is shown (a removed worktree).
	Notice string
	// MoreHref shows more commits; "" when all are shown.
	MoreHref string
	// Working is the uncommitted changes, as the graph's first row; nil
	// when there are none (or no worktree to have them).
	Working *WorkingRow
	Rows    []Row
	Open    *CommitView
	// OpenWorking is the uncommitted changes, open beside the graph.
	OpenWorking *WorkingView
	// PullRequest is the session branch's pull request control; nil on a
	// repository's page, or when the server cannot open pull requests.
	PullRequest *pullrequests.Panel
	// Git is what the head needs to show the session branch's sync state
	// (the browser's sync module paints it); nil on a repository's page.
	Git *GitHead
}

// GitHead names the worktree whose git state the head shows beside the
// branch: the chip, the second comparison with Fetch, and the panel.
type GitHead struct {
	ProjectID, WorkspaceID, Branch string
	// State is the worktree's git state when the server read it; nil leaves
	// the head to the browser. Upstream, Base and Fetched are painted from it.
	State            *domain.GitState
	Upstream, Base   gitsync.Chip
	BaseRef, Fetched string
}

// paint reads the chips and the fetch age off a state; nil or a gone worktree
// paints nothing.
func (g *GitHead) paint(s *domain.GitState, now time.Time) {
	if s == nil || s.Gone {
		return
	}
	g.State = s
	g.Upstream = gitsync.ChipOf(*s, now)
	g.Base = gitsync.BaseOf(*s)
	g.BaseRef = s.Base.Ref
	g.Fetched = g.Upstream.FetchedWord
}

// gitSeed is the head's seed for the sync module: the project to follow.
// The state is the one the server read, when it did.
type gitSeed struct {
	ProjectID string            `json:"project_id"`
	States    []domain.GitState `json:"git_states"`
}

func (g GitHead) seed() gitSeed {
	seed := gitSeed{ProjectID: g.ProjectID, States: []domain.GitState{}}
	if g.State != nil {
		seed.States = append(seed.States, *g.State)
	}
	return seed
}

// Crumb is one step back in the toolbar.
type Crumb struct{ Label, Href string }

// Row is one commit in the graph.
type Row struct {
	Hash, Short, Subject, Author, When, Href string
	Current                                  bool
	Refs                                     []RefView
	Graph                                    Graph
}

// WorkingRow is the uncommitted changes' row.
type WorkingRow struct {
	Href    string
	Summary string
	Current bool
	Graph   Graph
}

// RefView is a ref's badge. TaskHref, for an agent session's branch, links
// the session's task; Mine marks the branch of the session whose history
// this is.
type RefView struct {
	Name, Kind string
	Head, Mine bool
	TaskHref   string
}

// CommitView is the open commit.
type CommitView struct {
	Hash, Subject, Body, Author, Email, Date string
	Refs                                     []RefView
	Parents                                  []ParentLink
	Files                                    []FileView
	Added, Deleted                           int
	Summary                                  string
	// Diff is the commit's patch, painted first and taken over by codeDiff;
	// nil without a code browser.
	Diff *code.View
}

// WorkingView is the uncommitted changes, open.
type WorkingView struct {
	Summary string
	Files   []FileView
	// CodeHref is the session's Code tab at its Working scope: where the
	// uncommitted changes are read, file by file.
	CodeHref string
	// Diff is the uncommitted patch (scope working); nil without a code browser.
	Diff *code.View
}

// ParentLink opens a parent commit.
type ParentLink struct{ Short, Href string }

// FileView is one changed file. State is how it changed, in words, for
// uncommitted files ("modified", "untracked", …).
type FileView struct {
	Path, Added, Deleted, State string
	Binary                      bool
}
