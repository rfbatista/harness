package domain

import "time"

// SessionBranchState is where a branch on a session's Branches list stands
// (Glossary "Branches list"; contract "Harness server ↔ Web UI — branch
// requests and the Branches list" §1).
type SessionBranchState string

const (
	// SessionBranchSession is the session's own branch.
	SessionBranchSession    SessionBranchState = "session"
	SessionBranchApproved   SessionBranchState = "approved"
	SessionBranchDenied     SessionBranchState = "denied"
	SessionBranchDeleted    SessionBranchState = "deleted"
	SessionBranchUnapproved SessionBranchState = "unapproved"
)

// BranchBase counts a branch against the branch it started from.
type BranchBase struct {
	Ref    string `json:"ref"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
}

// Who pushed a branch.
const (
	PushedByPerson     = "person"
	PushedByAgent      = "agent"
	PushedByUnapproved = "unapproved"
)

// SessionBranchPush is one push of a branch to a remote.
type SessionBranchPush struct {
	Remote       string    `json:"remote"`
	RemoteBranch string    `json:"remote_branch"`
	At           time.Time `json:"at"`
	By           string    `json:"by"`
}

// SessionBranch is one row of a session's Branches list.
type SessionBranch struct {
	Name            string             `json:"name"`
	IsSessionBranch bool               `json:"is_session_branch"`
	State           SessionBranchState `json:"state"`
	// Base is nil when the branch is deleted or its base is unknown.
	Base       *BranchBase         `json:"base"`
	Pushed     []SessionBranchPush `json:"pushed"`
	RequestIDs []string            `json:"request_ids"`
	// FlaggedAt is set for an unapproved branch: when the after-turn check
	// found it.
	FlaggedAt *time.Time `json:"flagged_at"`
}

// UnattributedBranch is a finding no evidence ties to one session (ADR 0070).
type UnattributedBranch struct {
	RepositoryID        string    `json:"repository_id"`
	Branch              string    `json:"branch"`
	Kind                string    `json:"kind"` // "branch" or "push"
	CandidateSessionIDs []string  `json:"candidate_session_ids"`
	FoundAt             time.Time `json:"found_at"`
}

// SessionBranches is the Branches list of one session.
type SessionBranches struct {
	SessionID string          `json:"session_id"`
	Branches  []SessionBranch `json:"branches"`
}
