package domain

import "time"

// TicketStatus is the kanban lifecycle state of a ticket.
type TicketStatus string

const (
	TicketStatusBacklog    TicketStatus = "backlog"
	TicketStatusTodo       TicketStatus = "todo"
	TicketStatusInProgress TicketStatus = "in_progress"
	TicketStatusReview     TicketStatus = "review"
	TicketStatusDone       TicketStatus = "done"
)

// ErrTaskDoneIsThePersons refuses a session moving a task into done: only the
// person does that, after checking the work (ADR 0013).
func ErrTaskDoneIsThePersons() error {
	return &StructuredError{Code: "TASK_DONE_IS_THE_PERSONS",
		Message: "Only the person moves a task to done. Move it to review when the work is ready for them to check."}
}

// Ticket is a human-facing work item that agents act on, scoped to a Project.
// Over its lifetime a ticket may spawn one or more execution Tasks/Sessions
// (that dispatch link is not yet implemented).
type Ticket struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Status      TicketStatus `json:"status"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`

	// ArchitectSessionID and PendingReviews are derived when a ticket is
	// read, never stored: the task's architect session, and how many review
	// requests it has waiting on the person.
	ArchitectSessionID *string `json:"architect_session_id"`
	PendingReviews     int     `json:"pending_reviews"`

	// ArchivedAt is when the person archived the task; nil while it is on
	// the board. Only done tasks are archived.
	ArchivedAt *time.Time `json:"archived_at"`
	// StatusSince, LastMove and MergeState are derived when a ticket is read:
	// when it entered its status (its creation if it never moved), its most
	// recent status change, and, for review and done tasks, whether its
	// sessions' branches are merged.
	StatusSince time.Time   `json:"status_since"`
	LastMove    *LastMove   `json:"last_move"`
	MergeState  *MergeState `json:"merge_state"`
}

// CarriesMergeState reports whether the task shows a merge state: it is in
// review or done and still on the board.
func (t *Ticket) CarriesMergeState() bool {
	return t.ArchivedAt == nil && (t.Status == TicketStatusReview || t.Status == TicketStatusDone)
}

// LastMove is a task's most recent status change, as the board shows it.
type LastMove struct {
	Status TicketStatus  `json:"status"`
	By     StatusChanger `json:"by"`
	// SessionID and SessionLabel name the session that moved it, when By is
	// the architect or a session.
	SessionID    string    `json:"session_id,omitempty"`
	SessionLabel string    `json:"session_label,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	At           time.Time `json:"at"`
}

// MergeKind is how much of a task's code has landed in the default branch.
type MergeKind string

const (
	MergeMerged    MergeKind = "merged"
	MergePartial   MergeKind = "partial"
	MergeNotMerged MergeKind = "not_merged"
	MergeUnknown   MergeKind = "unknown"
	MergeNone      MergeKind = "none"
)

// MergeState counts a task's branches (those with a commit ahead of their
// base) and how many of them its repository's default branch contains.
type MergeState struct {
	State  MergeKind `json:"state"`
	Merged int       `json:"merged"`
	Total  int       `json:"total"`
}
