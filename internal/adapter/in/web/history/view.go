package history

import "operators-mcp/internal/adapter/in/web/shell"

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
}

// WorkingView is the uncommitted changes, open.
type WorkingView struct {
	Summary string
	Files   []FileView
}

// ParentLink opens a parent commit.
type ParentLink struct{ Short, Href string }

// FileView is one changed file. State is how it changed, in words, for
// uncommitted files ("modified", "untracked", …).
type FileView struct {
	Path, Added, Deleted, State string
	Binary                      bool
}
