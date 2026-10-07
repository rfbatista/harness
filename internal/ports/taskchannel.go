package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// The architect channel (see "Contract: Harness server ↔ Agent task tools —
// architect channel"). *taskchannel.Service satisfies the driving ports.

// TaskMessageInput is a message as a session writes it. The server fills in
// the ids, the task, the recipient and the delivery fields.
type TaskMessageInput struct {
	Kind        domain.TaskMessageKind
	Subject     string
	Body        string
	Status      domain.ReportStatus // status_report only
	Verdict     domain.Verdict      // a reply to a review_request only
	InReplyTo   string
	DocumentIDs []string // must be linked to the task
	ArtifactIDs []string // must be published on the task
}

// TaskMessageFilter narrows a task's messages: SessionID to those from or to
// that session, Since to those created after it. Zero values match all.
type TaskMessageFilter struct {
	SessionID string
	Since     time.Time
}

// ReviewRequestInput is a review the architect asks the person for.
type ReviewRequestInput struct {
	Subject        string
	Body           string
	AboutSessionID string // the delegate whose work it is; optional
	DocumentIDs    []string
	ArtifactIDs    []string
}

// TaskChannel is the session side of the architect channel: what the task
// tools call. Every session id is the caller's own, taken from the tool
// call's context, never from its arguments.
type TaskChannel interface {
	// TaskArchitect is the task's architect session; nil (and no error) when
	// the task has none.
	TaskArchitect(ctx context.Context, taskID string) (*domain.Session, error)
	// SessionRole is what the session is on its task.
	SessionRole(ctx context.Context, sessionID string) (domain.SessionRole, error)
	// SendToArchitect stores a message from a session to its task's architect
	// and delivers it if the architect is running. The message's Delivered
	// says whether it went in now; a busy architect gets it when its turn
	// ends, and a task_message event announces that. NO_ARCHITECT when the
	// task has none; INVALID_INPUT when the architect sends to itself.
	SendToArchitect(ctx context.Context, fromSessionID string, msg TaskMessageInput) (*domain.TaskMessage, error)
	// ReplyFromArchitect sends a reply from the architect to a session on its
	// task. ARCHITECT_ONLY, SESSION_NOT_ON_TASK, MESSAGE_NOT_FOUND.
	ReplyFromArchitect(ctx context.Context, architectID, toSessionID string, msg TaskMessageInput) (*domain.TaskMessage, error)
	// ListTaskMessages is every message on the task, oldest first, narrowed
	// by f. It applies no visibility rule: use ListSessionMessages for a
	// session's own view.
	ListTaskMessages(ctx context.Context, taskID string, f TaskMessageFilter) ([]*domain.TaskMessage, error)
	// ListSessionMessages is the task's messages as viewerSessionID may see
	// them: the architect sees all of them (narrowed by f.SessionID), any
	// other session only those from or to itself. Oldest first.
	ListSessionMessages(ctx context.Context, viewerSessionID string, f TaskMessageFilter) ([]*domain.TaskMessage, error)
	// RequestUserReview opens a pending review request for the person.
	// ARCHITECT_ONLY.
	RequestUserReview(ctx context.Context, architectID string, req ReviewRequestInput) (*domain.ReviewRequest, error)
	// WithdrawReview withdraws a pending review request. ARCHITECT_ONLY,
	// REVIEW_NOT_FOUND, REVIEW_NOT_PENDING.
	WithdrawReview(ctx context.Context, architectID, reviewID string) (*domain.ReviewRequest, error)
	// ListReviewRequests is the task's review requests, newest first; an
	// empty state matches all.
	ListReviewRequests(ctx context.Context, taskID string, state domain.ReviewState) ([]*domain.ReviewRequest, error)
	// RespondReview settles a pending review with the person's decision
	// (approved or changes_requested; the latter needs a note) and delivers
	// it to the architect. delivered says whether it went in now.
	RespondReview(ctx context.Context, reviewID string, decision domain.ReviewState, note string) (review *domain.ReviewRequest, delivered bool, err error)
	// SetTaskStatus moves the session's task. When the task has an architect
	// only the architect may: anyone else gets TASK_STATUS_OWNED_BY_ARCHITECT.
	// reason is optional, at most domain.MaxStatusReasonLen characters.
	SetTaskStatus(ctx context.Context, bySessionID string, status domain.TicketStatus, reason string) (*domain.Ticket, error)
	// SetStatusCheck sets the interval of the loop on one of the architect's
	// delegates: 0 pauses it, a value in [2, 240] resumes or retunes it. A
	// missing loop is created only for a delegate the architect started
	// directly. ARCHITECT_ONLY, SESSION_NOT_ON_TASK, INVALID_INPUT.
	SetStatusCheck(ctx context.Context, architectID, delegateID string, everyMinutes int) (*domain.StatusCheck, error)
	// ListStatusChecks is every loop on the task, ended ones included.
	ListStatusChecks(ctx context.Context, taskID string) ([]*domain.StatusCheck, error)
}

// TaskChannelUI is the person's side, served over HTTP (see "Contract:
// Harness server ↔ Web UI — architect channel API").
type TaskChannelUI interface {
	ListTaskMessages(ctx context.Context, taskID string, f TaskMessageFilter) ([]*domain.TaskMessage, error)
	ListReviewRequests(ctx context.Context, taskID string, state domain.ReviewState) ([]*domain.ReviewRequest, error)
	// ListProjectReviewRequests is a project's review requests in one state,
	// newest first: the board's inbox.
	ListProjectReviewRequests(ctx context.Context, projectID string, state domain.ReviewState) ([]*domain.ReviewRequest, error)
	RespondReview(ctx context.Context, reviewID string, decision domain.ReviewState, note string) (*domain.ReviewRequest, bool, error)
	// SetStatusCheckByPerson pauses (0), resumes or retunes an existing loop.
	// A person cannot create one: STATUS_CHECK_NOT_FOUND.
	SetStatusCheckByPerson(ctx context.Context, delegateID string, everyMinutes int) (*domain.StatusCheck, error)
}

// SessionDecorator fills the derived role fields (Role, ArchitectSessionID,
// StatusCheck) of sessions about to be shown.
type SessionDecorator interface {
	DecorateSessions(sessions ...*domain.Session)
}

// TicketDecorator fills the derived fields (ArchitectSessionID,
// PendingReviews) of tickets about to be shown.
type TicketDecorator interface {
	DecorateTickets(tickets ...*domain.Ticket)
}

// TurnDelivery puts text into a running session as a user turn. The
// orchestration implements it; the channel calls it.
type TurnDelivery interface {
	// Deliver sends text now when the session is idle, or queues it under key
	// to go in when the current turn ends; a later Deliver with the same key
	// replaces the queued text. onDelivered, if set, runs once when the text
	// actually went in. It never waits on the session. A session that is not
	// running answers SESSION_NOT_RUNNING and nothing is queued.
	Deliver(sessionID, key, text string, onDelivered func(at time.Time)) (deliveredNow bool, err error)
}

// ProjectChangeSink puts a change on its project's feed. The orchestration,
// which owns the feed, implements it.
type ProjectChangeSink interface {
	AnnounceChange(change ProjectChange)
}

// StatusCheckEvent is a status check as the feed announces it: the check, and
// on a firing when it fired and whether the turn went in.
type StatusCheckEvent struct {
	domain.StatusCheck
	FiredAt   *time.Time `json:"fired_at,omitempty"`
	Delivered *bool      `json:"delivered,omitempty"`
}

// TaskMessageRepository persists task messages. They outlive their sessions.
type TaskMessageRepository interface {
	Create(m *domain.TaskMessage) (*domain.TaskMessage, error)
	Get(id string) *domain.TaskMessage
	// List is the task's messages, oldest first.
	List(taskID string, f TaskMessageFilter) []*domain.TaskMessage
	MarkDelivered(id string, at time.Time) (*domain.TaskMessage, error)
	SetQueued(id string, queued bool) error
	// ListQueued is every message waiting in an outbox, oldest first.
	ListQueued() []*domain.TaskMessage
	// LastReport is the newest status_report a session sent, or nil.
	LastReport(fromSessionID string) *domain.TaskMessage
}

// ReviewFilter narrows review requests; TaskID or ProjectID is set.
type ReviewFilter struct {
	TaskID    string
	ProjectID string
	State     domain.ReviewState
}

// ReviewRequestRepository persists review requests. They outlive their
// sessions.
type ReviewRequestRepository interface {
	Create(r *domain.ReviewRequest) (*domain.ReviewRequest, error)
	Get(id string) *domain.ReviewRequest
	// Update rewrites state, response note, responded_at and updated_at.
	Update(r *domain.ReviewRequest) error
	// List is newest first.
	List(f ReviewFilter) []*domain.ReviewRequest
	CountPending(taskID string) int
}

// StatusCheckRepository persists status-check loops, one per delegate.
type StatusCheckRepository interface {
	// Save creates or replaces the delegate's loop.
	Save(c *domain.StatusCheck) error
	Get(delegateSessionID string) *domain.StatusCheck
	ListByTask(taskID string) []*domain.StatusCheck
	ListByArchitect(architectSessionID string) []*domain.StatusCheck
	// ListActive is every active loop, earliest next_at first.
	ListActive() []*domain.StatusCheck
}

// TaskStatusChangeRepository keeps the history of a task's status moves.
type TaskStatusChangeRepository interface {
	Append(c *domain.TaskStatusChange) error
	// ListByTask is oldest first.
	ListByTask(taskID string) []*domain.TaskStatusChange
}
