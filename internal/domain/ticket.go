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

// CodeTicketParentInvalid is the error code of a parent a ticket cannot
// take: missing, in another project, the ticket itself, or one that would
// nest the relation two levels deep.
const CodeTicketParentInvalid = "TICKET_PARENT_INVALID"

// CodeTicketHasChildren is the error code of deleting a feature task while
// it still has child tasks.
const CodeTicketHasChildren = "TICKET_HAS_CHILDREN"

// Ticket is a human-facing work item that agents act on, scoped to a Project.
// Over its lifetime a ticket may spawn one or more execution Tasks/Sessions
// (that dispatch link is not yet implemented).
//
// A ticket may have a parent of the same project, one level deep: the parent
// is a feature task and the ticket one of its child tasks (see CheckParent).
type Ticket struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Status      TicketStatus `json:"status"`
	// ParentID is the feature task this ticket belongs to; empty when none.
	// It is always on the wire so clients read one shape.
	ParentID  string    `json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ArchitectSessionID, PendingReviews and ChildrenCount are derived when
	// a ticket is read, never stored: the task's architect session, how many
	// review requests it has waiting on the person, and how many child tasks
	// it has (0 unless it is a feature task).
	ArchitectSessionID *string `json:"architect_session_id"`
	PendingReviews     int     `json:"pending_reviews"`
	ChildrenCount      int     `json:"children_count"`
}

// IsChild reports whether the ticket belongs to a feature task.
func (t *Ticket) IsChild() bool { return t.ParentID != "" }

// CheckParent is the pure part of the parent invariant: whether t (its ID
// empty while it is being created) may take parent, nil when the parent does
// not exist. The parent must be in t's project and must not be t itself; the
// relation is one level deep, so the parent cannot be a child and t cannot
// already have children (hasChildren, which the caller looks up). A refusal
// is CodeTicketParentInvalid.
func (t *Ticket) CheckParent(parent *Ticket, hasChildren bool) error {
	switch {
	case parent == nil:
		return parentInvalid("parent task not found")
	case parent.ProjectID != t.ProjectID:
		return parentInvalid("parent task belongs to another project")
	case t.ID != "" && parent.ID == t.ID:
		return parentInvalid("a task cannot be its own parent")
	case parent.IsChild():
		return parentInvalid("parent task is itself a child task: tasks nest one level deep")
	case hasChildren:
		return parentInvalid("a task with children cannot become a child task: tasks nest one level deep")
	}
	return nil
}

func parentInvalid(msg string) error {
	return &StructuredError{Code: CodeTicketParentInvalid, Message: msg}
}
