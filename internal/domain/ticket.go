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

// Ticket is a human-facing work item that agents act on, scoped to a Project.
// Over its lifetime a ticket may spawn one or more execution Tasks/Sessions
// (that dispatch link is not yet implemented).
type Ticket struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Status      TicketStatus `json:"status"`
	// ParentID is the feature task this one belongs to, one level deep and
	// in the same project (ADR 0016); empty when top-level. A task with
	// children is a feature task.
	ParentID  string    `json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ArchitectSessionID and PendingReviews are derived when a ticket is
	// read, never stored: the task's architect session, and how many review
	// requests it has waiting on the person.
	ArchitectSessionID *string `json:"architect_session_id"`
	PendingReviews     int     `json:"pending_reviews"`
}
