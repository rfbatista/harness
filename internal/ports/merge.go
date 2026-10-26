package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// BranchMerger merges a session's branch into its repository's default
// branch, in the repository's local checkout. The harness server, which runs
// unsandboxed, does it for the person (web UI) or for an architect (task
// tool): no agent needs write access to a main checkout. It never pushes,
// restarts nothing, and does not move the task's status.
type BranchMerger interface {
	// MergeSessionBranch merges with --no-ff. A refusal is a StructuredError
	// coded with one of domain.Merge*, and is recorded on the session like a
	// success: SESSION_NOT_FOUND is the only error that is not.
	MergeSessionBranch(ctx context.Context, sessionID string) (*domain.MergeResult, error)
	// MergeInto merges the session's branch into the target the person picked,
	// with the method they picked. A target that is not checked out in the
	// main checkout is merged without touching it. Refusals are recorded as
	// with MergeSessionBranch; INVALID_INPUT and SESSION_NOT_FOUND are not.
	MergeInto(ctx context.Context, sessionID string, req MergeRequest) (*domain.MergeResult, error)
	// MergeOffer says whether the session's branch can be merged into target
	// ("" is the default target) and answers what the Merge tab shows. It
	// reads local git and the stored session only.
	MergeOffer(ctx context.Context, sessionID, target string) (*MergeOffer, error)
	// BranchChangedSince reports whether the session's branch has a commit,
	// not yet on the default branch, whose committer date is later than since
	// (1s precision). An error means it could not be told.
	BranchChangedSince(ctx context.Context, sessionID string, since time.Time) (bool, error)
}

// MergeRequest is what the person picked for a server merge. The zero value
// is the architect's merge: the default target, a merge commit.
type MergeRequest struct {
	// Target is a local branch; empty is the default target.
	Target string
	// Method is "" or domain.MergeMethodMerge for --no-ff, or
	// domain.MergeMethodSquash.
	Method domain.MergeMethod
	// Message is the squash commit's message; required for a squash.
	Message string
}

// MergeTarget is a local branch a session's branch can be merged into.
type MergeTarget struct {
	Name string `json:"name"`
	// Default: the repository's default branch.
	Default bool `json:"default"`
	// CheckedOut: checked out in the main checkout. Dirty: it is, and that
	// checkout has uncommitted changes (tracked or untracked).
	CheckedOut bool `json:"checked_out"`
	Dirty      bool `json:"dirty"`
}

// MergeOffer is whether a merge applies to a session, into which branch, and
// what the Merge tab needs to show before the person presses the button.
type MergeOffer struct {
	Available bool   `json:"available"`
	Branch    string `json:"branch"`
	// Into is the target the offer is about: the requested one, else the
	// default target.
	Into string `json:"into"`
	// Reason is the domain.Merge code that says why it is not available.
	Reason string `json:"reason"`
	// Ahead is how many commits the branch has that Into lacks.
	Ahead int `json:"ahead"`
	// UncommittedChanges is the files changed in the session's worktree and
	// not committed: a warning, never a block.
	UncommittedChanges int `json:"uncommitted_changes"`
	// SuggestedMessage is the squash message: the task title, else the
	// branch's newest commit subject.
	SuggestedMessage string `json:"suggested_message"`
	// Targets is every local branch except the session's own, the default
	// branch first, then by name.
	Targets []MergeTarget `json:"targets"`
}

// CheckoutState is where a checkout is, read in one git run.
type CheckoutState struct {
	// Branch is the checked-out branch; empty on a detached HEAD.
	Branch string
	// Head is the commit checked out; empty in a repository with none.
	Head string
	// Tracked and Untracked are the paths with uncommitted changes to tracked
	// files and the paths of untracked files; ignored files are in neither.
	Tracked, Untracked []string
}

// Clean reports whether nothing is uncommitted.
func (s CheckoutState) Clean() bool { return len(s.Tracked) == 0 && len(s.Untracked) == 0 }

// BranchList is a repository's local branches and its default one.
type BranchList struct {
	Names   []string
	Default string
	// InOtherWorktrees are the branches checked out in a worktree other than
	// the main checkout (a session's): not merge targets.
	InOtherWorktrees []string
}

// GitMerger merges in a checkout (driven; gitcli implements it). Every run is
// bounded by a deadline.
type GitMerger interface {
	// HasBranch reports whether refs/heads/<branch> exists.
	HasBranch(ctx context.Context, dir, branch string) bool
	// Checkout reads dir's branch, HEAD and uncommitted paths. A directory
	// that is no git checkout is MERGE_NO_CHECKOUT.
	Checkout(ctx context.Context, dir string) (CheckoutState, error)
	// Merge merges branch into the branch checked out in dir with --no-ff and
	// answers the new HEAD. It refuses with MERGE_BRANCH_NOT_FOUND,
	// MERGE_ALREADY_MERGED (nothing changes) or MERGE_CONFLICT; a conflict,
	// like any failed merge, is aborted and checked to have left HEAD, index
	// and working tree as they were. It never pushes.
	Merge(ctx context.Context, dir, branch string) (head string, err error)
	// Branches names the local branches and the repository's default one
	// (origin/HEAD, else main, else master; empty when none), in one git run.
	Branches(ctx context.Context, dir string) (BranchList, error)
	// Ahead counts the commits branch has that target lacks.
	Ahead(ctx context.Context, dir, target, branch string) (int, error)
	// Subject is the subject line of branch's newest commit.
	Subject(ctx context.Context, dir, branch string) (string, error)
	// MergeInto merges branch into target without the main checkout's working
	// tree: the merge is made in git's object store and the target moves to
	// the result. A merge commit has two parents; a squash one, with message.
	// When target is checked out in dir (checkedOut), its files follow; when
	// not, the checkout's branch, HEAD, index and files are not touched. It
	// refuses like Merge does, and a conflict names its files in the error's
	// details. It never pushes.
	MergeInto(ctx context.Context, dir, target, branch string, method domain.MergeMethod, message string, checkedOut bool) (head string, err error)
	// BranchCommittedAfter reports whether branch has a commit that
	// defaultBranch lacks and whose committer date is later than since, at
	// one-second precision. It runs one git command.
	BranchCommittedAfter(ctx context.Context, dir, branch, defaultBranch string, since time.Time) (bool, error)
}

// SessionChangeAnnouncer puts a session's current record on its project's feed
// (the orchestration implements it), after something changed it outside the
// session's own events.
type SessionChangeAnnouncer interface {
	AnnounceSession(sessionID string)
}

// GitBranchNames reads what a session's branch name is checked against
// (driven; gitcli implements it).
type GitBranchNames interface {
	// RefFormatProblem is why name is not a valid branch name under git's own
	// rules (git check-ref-format --branch); empty when it is valid.
	RefFormatProblem(ctx context.Context, dir, name string) (string, error)
	// LocalBranches names every local branch, in one git run.
	LocalBranches(ctx context.Context, dir string) ([]string, error)
}

// BranchNameChecker checks a name a person typed for a session's branch
// (driving; *workspaces.BranchNames implements it). A check reads git only.
type BranchNameChecker interface {
	// CheckBranchName: ticketID, when set, makes the generated name carry the
	// task's slug. An unknown repository is REPOSITORY_NOT_FOUND.
	CheckBranchName(ctx context.Context, repositoryID, name, ticketID string) (*domain.BranchNameCheck, error)
}
