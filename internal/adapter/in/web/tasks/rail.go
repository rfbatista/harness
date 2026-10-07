// Package tasks serves the project and task pages, and builds the rail: the
// selected project's tasks (tickets) in kanban order, each with its live
// sessions. A task can run several sessions at once.
package tasks

import (
	"net/url"

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

// ReviewsHref is the project's review inbox.
func ReviewsHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/reviews"
}

// NewTaskHref is the project's new-task page.
func NewTaskHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/tasks/new"
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
// task are not on the rail. The seed is always there, so the browser follows
// the project even before its first task.
func BuildRail(projectID string, tasks []*domain.Ticket, list []*domain.Session, currentTaskID string) shell.Rail {
	rail := shell.Rail{Seed: seed(projectID, tasks, list), Current: currentTaskID}
	if len(tasks) == 0 {
		rail.Empty = "No tasks yet."
		return rail
	}
	for _, t := range tasks {
		rail.Reviews += t.PendingReviews
	}
	byTask := activityByTask(list)
	byStatus := map[domain.TicketStatus][]shell.Link{}
	var other []shell.Link
	for _, t := range tasks {
		link := card(projectID, t, byTask, currentTaskID)
		if known(t.Status) {
			byStatus[t.Status] = append(byStatus[t.Status], link)
		} else {
			other = append(other, link)
		}
	}
	for _, st := range kanban {
		if links := byStatus[st]; len(links) > 0 {
			rail.Groups = append(rail.Groups, shell.RailGroup{Label: sessions.StatusLabel(st), Links: links})
		}
	}
	if len(other) > 0 {
		rail.Groups = append(rail.Groups, shell.RailGroup{Label: "other", Links: other})
	}
	return rail
}

// card is a task as the rail links it and the board draws it.
func card(projectID string, t *domain.Ticket, byTask map[string]activity, currentTaskID string) shell.Link {
	a := byTask[t.ID]
	return shell.Link{
		TaskID:    t.ID,
		Label:     t.Title,
		Href:      Href(projectID, t.ID),
		Current:   t.ID == currentTaskID,
		Live:      a.live,
		Attention: a.attention || t.PendingReviews > 0,
		Reviews:   t.PendingReviews,
	}
}

func known(s domain.TicketStatus) bool {
	for _, k := range kanban {
		if k == s {
			return true
		}
	}
	return false
}
