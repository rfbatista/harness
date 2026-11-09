package domain

import (
	"fmt"
	"strings"
	"time"
)

// BranchRequestState is where a branch request stands (Glossary "Branch
// request"; contract "Harness server ↔ Web UI — branch requests and the
// Branches list" §1).
type BranchRequestState string

const (
	BranchRequestPending  BranchRequestState = "pending"
	BranchRequestApproved BranchRequestState = "approved"
	BranchRequestDenied   BranchRequestState = "denied"
	// BranchRequestUsed means the branch grant was consumed.
	BranchRequestUsed BranchRequestState = "used"
	// BranchRequestExpired means the session ended with an unused grant, or
	// while the request was still pending.
	BranchRequestExpired BranchRequestState = "expired"
)

// Codes of a branch request that cannot be answered.
const (
	BranchRequestNotFound = "BRANCH_REQUEST_NOT_FOUND"
	// BranchRequestAnswered: the request is no longer pending.
	BranchRequestAnswered = "BRANCH_REQUEST_ANSWERED"
)

// BranchRemote is the remote a push or a remote action names.
type BranchRemote struct {
	Name    string  `json:"name"`
	URL     string  `json:"url"`
	NewName *string `json:"new_name"`
}

// BranchRequest is the stored record of one branch action an agent session asks
// to make: only the person answers it, and an approval is a branch grant for
// exactly that action, used once.
type BranchRequest struct {
	ID            string           `json:"id"`
	SessionID     string           `json:"session_id"`
	TaskID        *string          `json:"task_id"`
	RepositoryID  string           `json:"repository_id"`
	SessionBranch string           `json:"session_branch"`
	Action        BranchActionKind `json:"action"`
	// Branch is the branch acted on; for a rename the old name.
	Branch  string  `json:"branch"`
	NewName *string `json:"new_name"`
	// StartPoint is set for create and switch.
	StartPoint   *BranchStart  `json:"start_point"`
	Remote       *BranchRemote `json:"remote"`
	RemoteBranch *string       `json:"remote_branch"`
	Commits      *int          `json:"commits"`
	// Command is the command line as the agent ran it, or "push_branch".
	Command string `json:"command"`
	// Via is "git" or "push_branch".
	Via         string             `json:"via"`
	Reason      string             `json:"reason"`
	State       BranchRequestState `json:"state"`
	Note        string             `json:"note"`
	RequestedAt time.Time          `json:"requested_at"`
	AnsweredAt  *time.Time         `json:"answered_at"`
}

// Matches says whether the request, once approved, is the grant for action: the
// same kind, branch, start point, remote with its URL, and remote branch: a
// push to another URL or under another name asks again.
func (r BranchRequest) Matches(a BranchAction) bool {
	newName := strOr(r.NewName)
	if r.Action == BranchRemoteRename && r.Remote != nil {
		newName = strOr(r.Remote.NewName)
	}
	if r.Action != a.Kind || r.Branch != a.Branch || newName != a.NewName {
		return false
	}
	if startRef(r.StartPoint) != startRef(a.Start) {
		return false
	}
	remote, url := "", ""
	if r.Remote != nil {
		remote, url = r.Remote.Name, r.Remote.URL
	}
	return remote == a.Remote && url == RedactURL(a.URL) && strOr(r.RemoteBranch) == a.RemoteBranch
}

// Label names what the request asks, for a line the person or the agent reads:
// "create feature-x", "rename a → b", "switch commit abc123".
func (r BranchRequest) Label() string {
	switch {
	case r.Action == BranchPush && r.Remote != nil:
		rb := r.Branch
		if r.RemoteBranch != nil {
			rb = *r.RemoteBranch
		}
		return fmt.Sprintf("push %s → %s/%s", r.Branch, r.Remote.Name, rb)
	case (r.Action == BranchRemoteAdd || r.Action == BranchRemoteSetURL) && r.Remote != nil:
		return fmt.Sprintf("%s %s %s", strings.ReplaceAll(string(r.Action), "_", " "), r.Remote.Name, r.Remote.URL)
	case r.Action == BranchRemoteRename && r.Remote != nil && r.Remote.NewName != nil:
		return fmt.Sprintf("remote rename %s → %s", r.Remote.Name, *r.Remote.NewName)
	case r.Action == BranchRemoteRemove && r.Remote != nil:
		return "remote remove " + r.Remote.Name
	case r.Action == BranchRename && r.NewName != nil:
		return fmt.Sprintf("%s %s → %s", r.Action, r.Branch, *r.NewName)
	case r.Branch == "" && r.StartPoint != nil:
		return fmt.Sprintf("%s commit %s", r.Action, r.StartPoint.Ref)
	}
	return fmt.Sprintf("%s %s", r.Action, r.Branch)
}

func startRef(s *BranchStart) string {
	if s == nil {
		return ""
	}
	return s.Kind + ":" + s.Ref
}

func strOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// BranchRequestFilter selects branch requests; empty fields match all.
type BranchRequestFilter struct {
	SessionID string
	TaskID    string
	State     BranchRequestState
}
