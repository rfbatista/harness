package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// BranchBaseline is the branches and remotes of a repository at one moment: a
// session's starting snapshot, or the repository's latest one the after-turn
// check compares with (ADR 0070).
type BranchBaseline struct {
	Branches   map[string]string `json:"branches"`
	RemoteRefs map[string]string `json:"remote_refs"`
	Remotes    []GitRemote       `json:"remotes"`
	TakenAt    time.Time         `json:"taken_at"`
}

// BranchStat is a branch counted against its base, read when the after-turn
// check ran, so the Branches list is served with no git call.
type BranchStat struct {
	SessionID, Branch, BaseRef string
	Ahead, Behind              int
}

// Kinds of branch finding.
const (
	FindingBranch = "branch"
	FindingPush   = "push"
)

// BranchFinding is something the after-turn check found that no branch request
// explains. SessionID is "" for an unattributed one.
type BranchFinding struct {
	ID           string
	RepositoryID string
	SessionID    string
	Branch       string
	Kind         string
	// Remote, RemoteBranch and Commit name a push.
	Remote, RemoteBranch, Commit string
	Candidates                   []string
	FoundAt                      time.Time
}

// BranchWatchStore keeps what the Branches list and the after-turn check need
// beside the branch requests (driven).
type BranchWatchStore interface {
	// Baseline reads a snapshot by key ("session:<id>" or "repo:<id>"); nil
	// when there is none.
	Baseline(ctx context.Context, key string) (*BranchBaseline, error)
	SaveBaseline(ctx context.Context, key string, b BranchBaseline) error
	// Stats reads the counts of the sessions' branches.
	Stats(ctx context.Context, sessionIDs ...string) ([]BranchStat, error)
	// SaveStats replaces the session's counts.
	SaveStats(ctx context.Context, sessionID string, stats []BranchStat) error
	// AddFinding stores f unless the same finding is stored already (same
	// repository, kind, branch, remote, remote branch and commit); it says
	// whether it stored it.
	AddFinding(ctx context.Context, f *BranchFinding) (bool, error)
	// Findings reads the findings of the sessions, or of a repository's
	// unattributed ones when sessionIDs is empty.
	Findings(ctx context.Context, repositoryID string, sessionIDs ...string) ([]BranchFinding, error)
}

// SessionBranchLists is the Branches list, read from stored state only
// (driving; web pages read it in process).
type SessionBranchLists interface {
	SessionBranches(ctx context.Context, sessionID string) ([]domain.SessionBranch, error)
	// TaskSessionBranches lists the sessions of the task, newest first.
	TaskSessionBranches(ctx context.Context, taskID string) ([]domain.SessionBranches, error)
}
