package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// BranchGrantRequest is what the git shim asks for before it runs a branch
// action: the action as the classifier read it and the directory git runs in
// (contract "Harness server ↔ Agent sessions — branch gate" §3).
type BranchGrantRequest struct {
	domain.BranchAction
	CWD string `json:"cwd"`
}

// BranchGrantAnswer says whether the shim may run the action. Reason names why
// it may not, when the harness knows.
type BranchGrantAnswer struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
	// RequestID is the branch request whose grant was used; the shim hands it
	// back when a push it let through succeeded.
	RequestID string `json:"request_id,omitempty"`
}

// BranchPushed is what the shim tells the gate after a push it let through
// exited 0: the request it ran under and where git ran.
type BranchPushed struct {
	RequestID string `json:"request_id"`
	CWD       string `json:"cwd"`
}

// BranchHookInput is the part of claude's PreToolUse hook input the gate reads.
type BranchHookInput struct {
	ToolName  string `json:"tool_name"`
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command     string `json:"command"`
		Description string `json:"description"`
	} `json:"tool_input"`
}

// BranchHookVerdict is the gate's answer to the asking hook. Empty (both
// fields "") lets the command go on; Decision is "allow" or "deny" otherwise.
type BranchHookVerdict struct {
	Decision string
	Reason   string
	// Context is what the agent is told besides: the person's note.
	Context string
}

// BranchRequestReader reads branch requests, newest first (driving; the web
// pages' first paint reads it in process).
type BranchRequestReader interface {
	ListBranchRequests(ctx context.Context, f domain.BranchRequestFilter) ([]domain.BranchRequest, error)
}

// BranchGate is the harness's guard on an agent session's branch actions
// (driving; ADR 0069). It never lets a branch action through on its own failure.
type BranchGate interface {
	// Grant uses up the session's branch grant for the action, if it has one.
	// A switch back to the session's own branch needs none.
	Grant(ctx context.Context, sessionID string, req BranchGrantRequest) BranchGrantAnswer
	// Pushed records a push the shim ran under a used grant, so the Branches
	// list explains it (ADR 0071).
	Pushed(ctx context.Context, sessionID string, p BranchPushed) error
	// Hook answers the asking hook: at once when the command has no branch
	// action, else after filing a branch request per action and waiting for
	// the person's answers. It returns when ctx ends, with a deny.
	Hook(ctx context.Context, sessionID string, in BranchHookInput) BranchHookVerdict
	// Answer is the person's answer to a pending branch request: decision is
	// "approve" or "deny".
	Answer(ctx context.Context, id, decision, note string) (*domain.BranchRequest, error)
	BranchRequestReader
}

// BranchRequestStore keeps branch requests (driven).
type BranchRequestStore interface {
	Add(ctx context.Context, r *domain.BranchRequest) error
	Get(ctx context.Context, id string) (*domain.BranchRequest, error)
	// Save stores r's state, note and answered_at.
	Save(ctx context.Context, r *domain.BranchRequest) error
	// List lists requests newest first.
	List(ctx context.Context, f domain.BranchRequestFilter) ([]domain.BranchRequest, error)
}

// BranchRequestWaiter shows a session as waiting for approval while it has a
// pending branch request (driven by the gate, implemented by the orchestration).
type BranchRequestWaiter interface {
	// BranchRequestFiled puts the session in waiting_approval.
	BranchRequestFiled(sessionID string)
	// BranchRequestSettled takes a request away; the last one puts the session
	// back to the status it had.
	BranchRequestSettled(sessionID string)
	// BranchRequestsRestored is the boot's recount: it sets the session's
	// pending-approvals count to the number of its stored pending requests,
	// never adding to what was stored (ADR 0078).
	BranchRequestsRestored(sessionID string, pending int)
}

// PushRecords keeps the pushes the harness made (driven; ADR 0071). The server
// push path writes one after each push that succeeded; the Branches list reads
// them.
type PushRecords interface {
	RecordPush(ctx context.Context, p domain.RecordedPush) error
	// PushesOf lists a session's recorded pushes, newest first.
	PushesOf(ctx context.Context, sessionID string) ([]domain.RecordedPush, error)
	// PushesIn lists the pushes made in a repository by any session, or by the
	// person, at or after since (the zero time lists all), newest first. Detection
	// uses it to explain a push in the repository's reflog.
	PushesIn(ctx context.Context, repositoryID string, since time.Time) ([]domain.RecordedPush, error)
}

// PushBranchInput is what the push_branch tool asks: the branch to push, the
// remote it goes to ("origin" when empty) and the branch it lands on (the
// branch's own name when empty).
type PushBranchInput struct {
	Branch       string
	Remote       string
	RemoteBranch string
	Reason       string
}

// BranchPusher files a push branch request for an agent session and returns
// at once (driving; ADR 0071). The person's answer, and after an approval the
// result of the push the harness made, reach the session as a turn. It never
// forces.
//
// Errors: INVALID_INPUT, BRANCH_NOT_FOUND, REMOTE_NOT_FOUND.
type BranchPusher interface {
	RequestPush(ctx context.Context, sessionID string, in PushBranchInput) (*domain.BranchRequest, error)
}
