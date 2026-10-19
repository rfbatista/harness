package tasks

import (
	"slices"
	"strconv"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// Board is the project's tasks as the kanban board's first paint: one column
// per status in board order (Statuses), every column present even when
// empty, each column's cards newest status_since first (the order the
// browser keeps, so the handover does not jump). The browser
// (web/src/modules/tasks, tasksBoard) keeps it live from the rail's seed.
type Board struct {
	Columns []Column
}

// Column is one status and its tasks.
type Column struct {
	Status, Label string
	Cards         []shell.Link
}

// BuildBoard groups a project's tasks by status with the same per-task
// activity the rail shows.
func BuildBoard(projectID string, tasks []*domain.Ticket, list []*domain.Session) Board {
	byTask := activityByTask(list)
	byStatus := map[string][]shell.Link{}
	tasks = slices.Clone(tasks)
	slices.SortStableFunc(tasks, func(a, b *domain.Ticket) int { return b.StatusSince.Compare(a.StatusSince) })
	for _, t := range tasks {
		byStatus[string(t.Status)] = append(byStatus[string(t.Status)], card(projectID, t, byTask, ""))
	}
	b := Board{}
	for _, st := range Statuses {
		b.Columns = append(b.Columns, Column{Status: st.Value, Label: st.Label, Cards: byStatus[st.Value]})
	}
	return b
}

// Empty: no task in any column.
func (b Board) Empty() bool {
	for _, c := range b.Columns {
		if len(c.Cards) > 0 {
			return false
		}
	}
	return true
}

// reviewsWord is a card's reviews badge: "1 review", "2 reviews". The
// browser's board (boardView.js) says the same.
func reviewsWord(n int) string {
	if n == 1 {
		return "1 review"
	}
	return strconv.Itoa(n) + " reviews"
}
