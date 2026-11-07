package ports

import (
	"context"

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

// BranchGate is the harness's guard on an agent session's branch actions
// (driving; ADR 0069). It never lets a branch action through on its own failure.
type BranchGate interface {
	// Grant uses up the session's branch grant for the action, if it has one.
	// A switch back to the session's own branch needs none.
	Grant(ctx context.Context, sessionID string, req BranchGrantRequest) BranchGrantAnswer
	// Hook answers the asking hook: at once when the command has no branch
	// action, else after filing a branch request per action and waiting for
	// the person's answers. It returns when ctx ends, with a deny.
	Hook(ctx context.Context, sessionID string, in BranchHookInput) BranchHookVerdict
	// Answer is the person's answer to a pending branch request: decision is
	// "approve" or "deny".
	Answer(ctx context.Context, id, decision, note string) (*domain.BranchRequest, error)
	// ListBranchRequests lists requests, newest first.
	ListBranchRequests(ctx context.Context, f domain.BranchRequestFilter) ([]domain.BranchRequest, error)
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
}
