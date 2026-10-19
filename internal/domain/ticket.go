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
}
