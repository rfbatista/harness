package tasks

import "operators-mcp/internal/domain"

// StatusOption is one task status as the forms offer it.
type StatusOption struct{ Value, Label string }

// Statuses are the board's states in order, as the forms list them; the
// browser's list is web/src/modules/tasks/domain/task.js.
var Statuses = []StatusOption{
	{string(domain.TicketStatusBacklog), "Backlog"},
	{string(domain.TicketStatusTodo), "Todo"},
	{string(domain.TicketStatusInProgress), "In progress"},
	{string(domain.TicketStatusReview), "Review"},
	{string(domain.TicketStatusDone), "Done"},
}
