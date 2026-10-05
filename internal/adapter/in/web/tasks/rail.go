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

// NewTaskHref is the project's new-task page.
func NewTaskHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/tasks/new"
}

// Href is a task's page.
func Href(projectID, taskID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/tasks/" + url.PathEscape(taskID)
}

// BuildRail groups a project's tasks by status and marks, per task, how many
// sessions are live and whether one waits on the developer. Sessions without a
// task are not on the rail.
func BuildRail(projectID string, tasks []*domain.Ticket, list []*domain.Session, currentTaskID string) shell.Rail {
	if len(tasks) == 0 {
		return shell.Rail{Empty: "No tasks yet."}
	}
	seed := &shell.RailSeed{ProjectID: projectID, Sessions: []shell.RailSession{}}
	for _, s := range list {
		if s.TicketID != "" {
			seed.Sessions = append(seed.Sessions, shell.RailSession{ID: s.ID, TicketID: s.TicketID, Status: string(s.Status), PendingApprovals: s.PendingApprovals})
		}
	}

	type activity struct {
		live      int
		attention bool
	}
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

	byStatus := map[domain.TicketStatus][]shell.Link{}
	var other []shell.Link
	for _, t := range tasks {
		a := byTask[t.ID]
		link := shell.Link{
			TaskID:    t.ID,
			Label:     t.Title,
			Href:      Href(projectID, t.ID),
			Current:   t.ID == currentTaskID,
			Live:      a.live,
			Attention: a.attention,
		}
		if known(t.Status) {
			byStatus[t.Status] = append(byStatus[t.Status], link)
		} else {
			other = append(other, link)
		}
	}

	rail := shell.Rail{Seed: seed}
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

func known(s domain.TicketStatus) bool {
	for _, k := range kanban {
		if k == s {
			return true
		}
	}
	return false
}
