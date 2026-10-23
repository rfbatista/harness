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
	// ArchivedCount counts the project's archived tasks, for the
	// "Archived (N)" link.
	ArchivedCount int
}

// Column is one status and its tasks.
type Column struct {
	Status, Label string
	Cards         []Card
}

// Card is one task on the board: the rail's link and activity, plus the
// feature cues (ADR 0016): Parent is the feature task's title on a child
// ("" when top-level, or when the parent is not among the project's tasks);
// Children counts a feature task's children. The browser's toCard
// (boardView.js) derives the same from the store's task list;
// web/testdata/views/feature-cues.json pins the words.
type Card struct {
	shell.Link
	Parent   string
	Children int
}

// ChildrenWord is the children badge: "1 task", "3 tasks"; "" for none. The
// browser's childrenWord (tasks/domain/feature.js) says the same.
func ChildrenWord(n int) string {
	if n < 1 {
		return ""
	}
	if n == 1 {
		return "1 task"
	}
	return strconv.Itoa(n) + " tasks"
}

// BuildBoard groups a project's tasks by status with the same per-task
// activity the rail shows, and the feature cues from the same list.
// Archived tasks are not on it; each column lists its newest move first.
func BuildBoard(projectID string, tasks []*domain.Ticket, list []*domain.Session) Board {
	// unarchived returns a new slice, so sorting it leaves the caller's alone.
	tasks = unarchived(tasks)
	byTask := activityByTask(list)
	slices.SortStableFunc(tasks, func(a, b *domain.Ticket) int { return b.StatusSince.Compare(a.StatusSince) })
	titles := make(map[string]string, len(tasks))
	children := map[string]int{}
	for _, t := range tasks {
		titles[t.ID] = t.Title
	}
	for _, t := range tasks {
		if _, known := titles[t.ParentID]; t.ParentID != "" && known {
			children[t.ParentID]++
		}
	}
	byStatus := map[string][]Card{}
	for _, t := range tasks {
		byStatus[string(t.Status)] = append(byStatus[string(t.Status)], Card{
			Link: card(projectID, t, byTask, ""), Parent: titles[t.ParentID], Children: children[t.ID],
		})
	}
	b := Board{}
	for _, st := range Statuses {
		b.Columns = append(b.Columns, Column{Status: st.Value, Label: st.Label, Cards: byStatus[st.Value]})
	}
	return b
}

// unarchived leaves out the archived tasks: neither the board nor the rail
// shows them, whatever list they are handed.
func unarchived(tasks []*domain.Ticket) []*domain.Ticket {
	out := tasks[:0:0]
	for _, t := range tasks {
		if t.ArchivedAt == nil {
			out = append(out, t)
		}
	}
	return out
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
