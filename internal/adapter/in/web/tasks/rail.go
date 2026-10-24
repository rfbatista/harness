// Package tasks serves the project and task pages, and builds the rail: the
// selected project's tasks (tickets) in kanban order, each with its live
// sessions. A task can run several sessions at once.
package tasks

import (
	"net/url"
	"slices"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// kanban is the rail's group order: what is moving first, done last (as the
// TUI's tasks screen orders them).
var kanban = []domain.TicketStatus{
	domain.TicketStatusInProgress,
	domain.TicketStatusReview,
	domain.TicketStatusTodo,
	domain.TicketStatusBacklog,
	domain.TicketStatusDone,
}

// DoneCap is how many done tasks the rail shows before "Show N more"; the
// browser's is DONE_CAP in web/src/modules/tasks/domain/rail.js.
const DoneCap = 5

// BoardHref is the project's board.
func BoardHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID)
}

// ReviewsHref is the project's review inbox.
func ReviewsHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/reviews"
}

// NewTaskHref is the project's new-task page.
func NewTaskHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/tasks/new"
}

// ArchivedHref is the project's Archived view.
func ArchivedHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/archived"
}

// Href is a task's page.
func Href(projectID, taskID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/tasks/" + url.PathEscape(taskID)
}

// activity is what the rail and the board show per task.
type activity struct {
	live      int
	attention bool
}

// activityByTask counts, per task, the sessions that are live and whether one
// waits on the developer. Sessions without a task are left out.
func activityByTask(list []*domain.Session) map[string]activity {
	byTask := map[string]activity{}
	for _, s := range list {
		if s.TicketID == "" {
			continue
		}
		a := byTask[s.TicketID]
		if sessions.IsLive(s) {
			a.live++
		}
		a.attention = a.attention || sessions.NeedsYou(s)
		byTask[s.TicketID] = a
	}
	return byTask
}

// seed is what the browser starts from: the project's sessions on tasks, and
// its tasks.
func seed(projectID string, tasks []*domain.Ticket, list []*domain.Session) *shell.RailSeed {
	s := &shell.RailSeed{ProjectID: projectID, Sessions: []shell.RailSession{}, Tasks: tasks}
	if s.Tasks == nil {
		s.Tasks = []*domain.Ticket{}
	}
	for _, x := range list {
		if x.TicketID != "" {
			s.Sessions = append(s.Sessions, shell.RailSession{ID: x.ID, TicketID: x.TicketID, Status: string(x.Status), PendingApprovals: x.PendingApprovals})
		}
	}
	return s
}

// BuildRail groups a project's tasks by status and marks, per task, how many
// sessions are live and whether one waits on the developer. Sessions without a
// task are not on the rail. Every known status has its group, empty or not;
// collapsed holds the groups the person folded (shell.ParseCollapsed). The
// seed is always there, so the browser follows the project even before its
// first task. Archived tasks are neither on the rail nor in its seed.
func BuildRail(projectID string, tasks []*domain.Ticket, list []*domain.Session, currentTaskID string, collapsed map[string]bool) shell.Rail {
	tasks = unarchived(tasks)
	rail := shell.Rail{Seed: seed(projectID, tasks, list), Current: currentTaskID}
	if len(tasks) == 0 {
		rail.Empty = "No tasks yet."
		return rail
	}
	for _, t := range tasks {
		rail.Reviews += t.PendingReviews
	}
	byTask := activityByTask(list)
	byStatus := map[domain.TicketStatus][]*domain.Ticket{}
	var other []*domain.Ticket
	for _, t := range tasks {
		if known(t.Status) {
			byStatus[t.Status] = append(byStatus[t.Status], t)
		} else {
			other = append(other, t)
		}
	}
	links := func(ts []*domain.Ticket) []shell.Link {
		out := make([]shell.Link, 0, len(ts))
		for _, t := range ts {
			out = append(out, card(projectID, t, byTask, currentTaskID))
		}
		return out
	}
	for _, st := range kanban {
		g := shell.RailGroup{Label: statusLabel(st), Key: string(st), Total: len(byStatus[st])}
		if st == domain.TicketStatusDone {
			g.Links, g.More = capDone(links(byDoneOrder(byStatus[st])))
		} else {
			g.Links = links(byStatus[st])
		}
		g.Collapsed = collapsed[g.Key] && g.Total > 0
		rail.Groups = append(rail.Groups, g)
	}
	if len(other) > 0 {
		rail.Groups = append(rail.Groups, shell.RailGroup{Label: "Other", Key: "other", Total: len(other), Collapsed: collapsed["other"], Links: links(other)})
	}
	return rail
}

// byDoneOrder is the most recently updated first; ties keep creation order.
func byDoneOrder(ts []*domain.Ticket) []*domain.Ticket {
	out := slices.Clone(ts)
	slices.SortStableFunc(out, func(a, b *domain.Ticket) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return out
}

// capDone splits Done at DoneCap. The open task and the tasks that need the
// person stay shown whatever their age: the rail never hides them.
func capDone(all []shell.Link) (shown, more []shell.Link) {
	for i, l := range all {
		if i < DoneCap || l.Current || l.Attention {
			shown = append(shown, l)
		} else {
			more = append(more, l)
		}
	}
	return shown, more
}

func statusLabel(st domain.TicketStatus) string {
	for _, s := range Statuses {
		if s.Value == string(st) {
			return s.Label
		}
	}
	return string(st)
}

// card is a task as the rail links it and the board draws it.
func card(projectID string, t *domain.Ticket, byTask map[string]activity, currentTaskID string) shell.Link {
	a := byTask[t.ID]
	l := shell.Link{
		TaskID:    t.ID,
		Label:     t.Title,
		Href:      Href(projectID, t.ID),
		Current:   t.ID == currentTaskID,
		Live:      a.live,
		Attention: a.attention || t.PendingReviews > 0,
		Reviews:   t.PendingReviews,
	}
	if m := mergeBadge(string(t.Status), cardFactsOf(t).merge); m != nil {
		l.Merge, l.MergeText = m.State, m.Rail
	}
	return l
}

func known(s domain.TicketStatus) bool {
	for _, k := range kanban {
		if k == s {
			return true
		}
	}
	return false
}
