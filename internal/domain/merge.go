package domain

import "time"

// The codes a merge of a session's branch is refused or fails with. Each is
// distinct, so the person and an architect can tell the reasons apart.
const (
	// MergeNoBranch: the session has no branch (it ran before worktree
	// isolation, or in no worktree).
	MergeNoBranch = "MERGE_NO_BRANCH"
	// MergeNoCheckout: the session's repository has no local checkout to
	// merge in, or it is gone from the disk.
	MergeNoCheckout = "MERGE_NO_CHECKOUT"
	// MergeCheckoutDirty: the checkout has uncommitted changes.
	MergeCheckoutDirty = "MERGE_CHECKOUT_DIRTY"
	// MergeWrongBranch: the checkout is not on the default branch.
	MergeWrongBranch = "MERGE_WRONG_BRANCH"
	// MergeAlreadyMerged: the branch is already part of the default branch.
	MergeAlreadyMerged = "MERGE_ALREADY_MERGED"
	// MergeConflict: the merge conflicts; it was aborted and the checkout is
	// as it was.
	MergeConflict = "MERGE_CONFLICT"
	// MergeBranchNotFound: the session's branch no longer exists.
	MergeBranchNotFound = "MERGE_BRANCH_NOT_FOUND"
	// MergeTargetNotFound: the target is not a local branch, or is the
	// session's own branch.
	MergeTargetNotFound = "MERGE_TARGET_NOT_FOUND"
	// MergeFailed: git failed in a way none of the above names (an unknown
	// committer identity, a failing hook, a timeout); the merge was aborted.
	MergeFailed = "MERGE_FAILED"
)

// MergeMethod is how a branch is merged: a merge commit that keeps every
// commit, or a squash into one new commit. Rebase is not offered because it
// rewrites the agent's branch (ADR 0035).
type MergeMethod string

const (
	MergeMethodMerge  MergeMethod = "merge"
	MergeMethodSquash MergeMethod = "squash"
)

// MergeResult is how the newest attempt to merge a session's branch ended: it
// is recorded on the session, so it outlives the request and shows on the
// session. A merged result carries the new HEAD; a refusal its code and
// reason.
type MergeResult struct {
	At     time.Time `json:"at"`
	Merged bool      `json:"merged"`
	// Into is the default branch the merge was made into (or, for a refusal
	// before it was known, empty).
	Into string `json:"into,omitempty"`
	// Method is how it was merged, or tried: "merge" for every record written
	// before squashes existed.
	Method MergeMethod `json:"method"`
	// Head is the default branch's HEAD after the merge.
	Head string `json:"head,omitempty"`
	// Code and Message are the refusal: one of the Merge codes and a readable
	// reason.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	// SquashedIntoDefault is set on a successful squash into the repository's
	// default branch. It is not part of the record on the wire: the session
	// keeps it for good, so the task's merge state still counts the session as
	// merged after a later refusal.
	SquashedIntoDefault bool `json:"-"`
}

// MergeError is a coded merge refusal.
func MergeError(code, message string) error {
	return &StructuredError{Code: code, Message: message}
}
