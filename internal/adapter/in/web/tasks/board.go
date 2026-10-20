package tasks

import (
	"slices"
	"strconv"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// now is the board's clock; tests stop it.
var now = time.Now

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

// Waiting: Review holds tasks, which wait for the person's check.
func (c Column) Waiting() bool { return c.Status == "review" && len(c.Cards) > 0 }

// EmptyText is what the column says when it has no task.
func (c Column) EmptyText() string {
	switch c.Status {
	case "review":
		return "Nothing waiting for your check."
	case "done":
		return "No done tasks. Drag a checked task here from Review."
	}
	return "No tasks"
}

// Card is a task as the board draws it: its link and activity, then its
// signals, its last move and its time in column.
type Card struct {
	shell.Link
	Status string
	// LiveState is the dot: "waiting" when a session waits on the person,
	// "running" when one runs, "" when quiet. Reviews have their own badge.
	LiveState, LiveWord string
	Merge               *MergeBadge
	Lead, Who, Reason   string
	// At is when the task entered its column, to the minute; "" when unknown.
	At    string
	Age   string
	Stale bool
}

// HasSignals: the card has a live dot, a reviews badge or a merge badge.
func (c Card) HasSignals() bool { return c.LiveState != "" || c.Reviews > 0 || c.Merge != nil }

// BuildBoard groups a project's tasks by status with the same per-task
// activity the rail shows. Archived tasks are not on it.
func BuildBoard(projectID string, tasks []*domain.Ticket, list []*domain.Session) Board {
	// unarchived returns a new slice, so sorting it leaves the caller's alone.
	tasks = unarchived(tasks)
	byTask := activityByTask(list)
	at := now()
	byStatus := map[string][]Card{}
	slices.SortStableFunc(tasks, func(a, b *domain.Ticket) int { return b.StatusSince.Compare(a.StatusSince) })
	for _, t := range tasks {
		byStatus[string(t.Status)] = append(byStatus[string(t.Status)], boardCard(projectID, t, byTask, at))
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

// boardCard is a task's card at a moment.
func boardCard(projectID string, t *domain.Ticket, byTask map[string]activity, at time.Time) Card {
	a := byTask[t.ID]
	f := cardFactsOf(t)
	status := string(t.Status)
	c := Card{Link: card(projectID, t, byTask, ""), Status: status, Merge: mergeBadge(status, f.merge)}
	switch {
	case a.attention:
		c.LiveState, c.LiveWord = "waiting", "waiting"
	case a.live > 0:
		c.LiveState, c.LiveWord = "running", strconv.Itoa(a.live)+" live"
	}
	c.Lead, c.Who, c.Reason = lastMoveLine(f.lastMove)
	c.Age, c.Stale = cardAge(status, f.statusSince, at)
	if !f.statusSince.IsZero() {
		c.At = exactTime(f.statusSince)
	}
	return c
}

// cardFacts is what a card reads beyond the task's status and title.
type cardFacts struct {
	statusSince time.Time
	lastMove    *lastMove
	merge       *mergeState
}

// cardFactsOf reads a task's card facts from its Ticket. A Ticket read
// without them (status_since zero) is in its column since it was created.
func cardFactsOf(t *domain.Ticket) cardFacts {
	f := cardFacts{statusSince: t.StatusSince}
	if f.statusSince.IsZero() {
		f.statusSince = t.CreatedAt
	}
	if m := t.LastMove; m != nil {
		f.lastMove = &lastMove{Status: string(m.Status), By: string(m.By), SessionLabel: m.SessionLabel, Reason: m.Reason, At: m.At}
	}
	if m := t.MergeState; m != nil {
		f.merge = &mergeState{State: string(m.State), Merged: m.Merged, Total: m.Total}
	}
	return f
}

// reviewsWord is a card's reviews badge: "1 review", "2 reviews". The
// browser's board (boardView.js) says the same.
func reviewsWord(n int) string {
	if n == 1 {
		return "1 review"
	}
	return strconv.Itoa(n) + " reviews"
}
