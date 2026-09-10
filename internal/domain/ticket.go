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
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}
