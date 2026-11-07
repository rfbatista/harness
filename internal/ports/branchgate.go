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

// BranchGate is the harness's guard on an agent session's branch actions
// (driving; ADR 0069). It never errors: a gate that cannot decide refuses.
type BranchGate interface {
	// Grant uses up the session's branch grant for the action, if it has one.
	// A switch back to the session's own branch needs none.
	Grant(ctx context.Context, sessionID string, req BranchGrantRequest) BranchGrantAnswer
}
