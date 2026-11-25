// Package tasks serves the project and task pages, and builds what the rail
// island starts from: the selected project's tasks (tickets) and their live
// sessions. A task can run several sessions at once.
package tasks

import (
	"net/url"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

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
			s.Sessions = append(s.Sessions, shell.RailSession{ID: x.ID, TicketID: x.TicketID, Status: string(x.Status), PendingApprovals: x.PendingApprovals, OpenInterviewCount: x.OpenInterviewCount})
		}
	}
	return s
}

// BuildRail is what the rail island starts from: the seed (the project's
// sessions on tasks and its tasks) and the open task. The island groups the
// tasks itself (web/app/src/features/rail), by the rules the board's first paint
// shares. The seed is always there, so the browser follows the project even
// before its first task. Archived tasks are not on the rail: the seed carries
// them only as a child archived on its own, which the board lists inside its
// feature's card. Empty says the project has no task yet.
func BuildRail(projectID string, tasks []*domain.Ticket, list []*domain.Session, currentTaskID string) shell.Rail {
	rail := shell.Rail{Seed: seed(projectID, onBoard(tasks), list), Current: currentTaskID}
	if len(unarchived(tasks)) == 0 {
		rail.Empty = "No tasks yet."
	}
	return rail
}

// card is a task as the board draws it.
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

func statusLabel(st domain.TicketStatus) string {
	for _, s := range Statuses {
		if s.Value == string(st) {
			return s.Label
		}
	}
	return string(st)
}
