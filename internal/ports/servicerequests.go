package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// Service requests (ADR 0083): the changes agents ask of a project's services,
// which the person approves. See "Contract: Harness server ↔ Web UI — services"
// §13 and "Contract: Harness server ↔ Agent sessions — the services MCP server"
// "Service requests".

// ServiceRequestStore keeps service requests, with their values (driven).
type ServiceRequestStore interface {
	// Add stores a new request and sets its id.
	Add(ctx context.Context, r *domain.ServiceRequest) error
	// Get is the request, nil for none.
	Get(ctx context.Context, id string) (*domain.ServiceRequest, error)
	// Save replaces the stored request.
	Save(ctx context.Context, r *domain.ServiceRequest) error
	// List lists requests newest first.
	List(ctx context.Context, f domain.ServiceRequestFilter) ([]domain.ServiceRequest, error)
}

// ServiceRequestInput is an agent's request, already validated by the tool
// that files it.
type ServiceRequestInput struct {
	SessionID   string
	Action      domain.ServiceRequestAction
	ServiceID   string
	ServiceName string
	Reason      string
	// Worktree is "own" for a run in the session's worktree, "" for the
	// repository's main checkout.
	Worktree string
	// Summary, Proposed, Changes and EnvChanges describe a create or an update:
	// the readable line, the definition after it, each changed field, and the
	// variables set or unset. Create and update are performed from Proposed.
	Summary    string
	Proposed   *domain.ServiceInput
	Changes    []domain.ServiceRequestChange
	EnvChanges []domain.ServiceRequestEnv
	// Env are the run overrides of a start or restart; EnvGiven says they were
	// named (a restart repeats the last run's when they were not).
	Env      map[string]string
	EnvGiven bool
}

// ServiceRequestOutcome is what an approved action answered.
type ServiceRequestOutcome struct {
	Service        *domain.Service
	AlreadyRunning bool
	RestartNeeded  bool
}

// ServiceRequestWaiter shows a session as waiting for approval while it has a
// pending service request, as for a branch request (driven; the orchestration
// implements it).
type ServiceRequestWaiter interface {
	ServiceRequestFiled(sessionID string)
	ServiceRequestSettled(sessionID string)
	// ServiceRequestsRestored is the boot's recount: it adds the session's
	// stored pending service requests to its pending-approvals count, after the
	// branch gate set the count to its own (ADR 0078).
	ServiceRequestsRestored(sessionID string, pending int)
}

// ServiceRequests files, answers and lists service requests (driving).
type ServiceRequests interface {
	// Submit files the request and blocks until the person answers it, with no
	// timeout. It returns the approved action's outcome; a denied request is
	// SERVICE_REQUEST_DENIED, one whose session ended SERVICE_REQUEST_EXPIRED,
	// and ctx ending ends the wait and leaves the request pending.
	Submit(ctx context.Context, in ServiceRequestInput) (*ServiceRequestOutcome, error)
	// Answer is the person's answer: decision is "approve" or "deny". Approving
	// performs the action at once. A request that is not pending is
	// REQUEST_NOT_PENDING.
	Answer(ctx context.Context, id, decision, note string) (*domain.ServiceRequest, error)
	List(ctx context.Context, f domain.ServiceRequestFilter) ([]domain.ServiceRequest, error)
	// Restore is what a restart leaves to rebuild, once at boot: a request whose
	// session ended while the server was down expires, a request that was
	// approved but never carried out fails (it is not replayed), and each live
	// session's pending count is raised by its pending requests.
	Restore(ctx context.Context) error
}
