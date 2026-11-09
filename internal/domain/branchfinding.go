package domain

// BranchFindingKind is what the after-turn check found that no branch request
// explains (ADR 0070).
type BranchFindingKind string

const (
	// FindingBranch is a branch that appeared.
	FindingBranch BranchFindingKind = "branch"
	// FindingPush is a push that went around the gate.
	FindingPush BranchFindingKind = "push"
	// FindingRemote is a remote that was added, or whose URL changed.
	FindingRemote BranchFindingKind = "remote"
	// FindingDelete is a branch a session made or touched that disappeared.
	FindingDelete BranchFindingKind = "deleted"
	// FindingRename is such a branch that reappeared under another name at the
	// same commit.
	FindingRename BranchFindingKind = "renamed"
)
