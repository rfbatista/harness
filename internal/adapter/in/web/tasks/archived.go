package tasks

import (
	"strconv"

	"operators-mcp/internal/domain"
)

// Archived is the Archived view's first paint: the project's archived tasks,
// newest archived first, and the seed the browser (tasksArchived) keeps live.
type Archived struct {
	Rows []ArchivedRow
	Seed ArchivedSeed
}

// ArchivedSeed is what tasksArchived starts from: the archived tickets in the
// API's shape.
type ArchivedSeed struct {
	ProjectID string           `json:"project_id"`
	Tickets   []*domain.Ticket `json:"tickets"`
}

// ArchivedRow is one archived task: its link, when it was finished and
// archived, and its last known merge badge.
type ArchivedRow struct {
	TaskID, Title, Href string
	Finished, Archived  string
	Merge               *MergeBadge
}

// BuildArchived reads the archived tasks as the view draws them.
func BuildArchived(projectID string, list []*domain.Ticket) Archived {
	a := Archived{Seed: ArchivedSeed{ProjectID: projectID, Tickets: list}}
	if a.Seed.Tickets == nil {
		a.Seed.Tickets = []*domain.Ticket{}
	}
	for _, t := range list {
		row := ArchivedRow{TaskID: t.ID, Title: t.Title, Href: Href(projectID, t.ID), Merge: mergeBadge(string(t.Status), cardFactsOf(t).merge)}
		if !t.StatusSince.IsZero() {
			row.Finished = "Finished " + t.StatusSince.Local().Format("Jan 2")
		}
		if t.ArchivedAt != nil {
			row.Archived = "Archived " + t.ArchivedAt.Local().Format("Jan 2")
		}
		a.Rows = append(a.Rows, row)
	}
	return a
}

// Summary is the line above the list; "" when nothing is archived. The
// browser says the same (archivedView.js).
func (a Archived) Summary() string {
	n := len(a.Rows)
	switch n {
	case 0:
		return ""
	case 1:
		return "1 archived task. Restore puts a task back in Done. Delete removes it and everything it holds."
	}
	return strconv.Itoa(n) + " archived tasks. Restore puts a task back in Done. Delete removes it and everything it holds."
}
