package tasks

import (
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func TestBuildFeatureListsChildrenInOrderWithTheirDocumentsAndThePRD(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	feature := &domain.Ticket{ID: "f1", ProjectID: "p1", Title: "Exports", Status: domain.TicketStatusInProgress}
	tickets := []*domain.Ticket{
		{ID: "c2", ProjectID: "p1", Title: "PDF export", Status: domain.TicketStatusDone, ParentID: "f1"},
		feature,
		{ID: "t9", ProjectID: "p1", Title: "Plain", Status: domain.TicketStatusTodo},
		{ID: "c1", ProjectID: "p1", Title: "CSV export", Status: domain.TicketStatusInProgress, ParentID: "f1"},
		{ID: "x", ProjectID: "p1", Title: "Someone else's child", Status: domain.TicketStatusTodo, ParentID: "t9"},
	}
	docs := []*domain.Document{
		{ID: "d-old", Title: "PRD: Exports", UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: "d-notes", Title: "Notes", UpdatedAt: now.Add(-time.Minute)},
		{ID: "d-prd", Title: "prd: Exports v2", UpdatedAt: now.Add(-time.Hour)},
	}
	docsOf := func(id string) []*domain.Document {
		if id != "c1" {
			return nil
		}
		return []*domain.Document{
			{ID: "b-old", Title: "Brief: CSV (draft)", UpdatedAt: now.Add(-time.Hour)},
			{ID: "b-new", Title: "Brief: CSV", UpdatedAt: now.Add(-5 * time.Minute)},
		}
	}
	v := BuildFeature("p1", feature, tickets, docs, docsOf, now)
	if v == nil {
		t.Fatal("a task with children is a feature")
	}
	if v.PRD == nil || v.PRD.Title != "prd: Exports v2" || v.PRD.Href != "/projects/p1/tasks/f1/documents/d-prd" || v.PRD.Updated != "1h" {
		t.Errorf("PRD = %+v (the newest PRD: document wins, whatever its case)", v.PRD)
	}
	var got []string
	for _, c := range v.Children {
		line := c.ID + "/" + c.StatusLabel + "/" + c.Href
		for _, d := range c.Documents {
			line += " " + d.Title + "@" + d.Href
		}
		got = append(got, line)
	}
	want := []string{
		"c2/done//projects/p1/tasks/c2",
		"c1/in progress//projects/p1/tasks/c1 Brief: CSV@/projects/p1/tasks/c1/documents/b-new Brief: CSV (draft)@/projects/p1/tasks/c1/documents/b-old",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("children =\n%v\nwant\n%v", got, want)
	}
	if BuildFeature("p1", &domain.Ticket{ID: "t9"}, tickets, nil, nil, now).PRD != nil {
		t.Error("no PRD document, no PRD link")
	}
	if BuildFeature("p1", &domain.Ticket{ID: "c1"}, tickets, docs, docsOf, now) != nil {
		t.Error("a task without children has no feature section")
	}
}

func TestParentOfLinksTheFeatureTaskWhenItIsInTheProject(t *testing.T) {
	tickets := []*domain.Ticket{{ID: "f1", ProjectID: "p1", Title: "Exports"}}
	if p := ParentOf("p1", &domain.Ticket{ID: "c1", ParentID: "f1"}, tickets); p == nil || p.Title != "Exports" || p.Href != "/projects/p1/tasks/f1" {
		t.Errorf("parent = %+v", p)
	}
	if ParentOf("p1", &domain.Ticket{ID: "c1", ParentID: "gone"}, tickets) != nil {
		t.Error("a parent that is not among the project's tasks is no link")
	}
	if ParentOf("p1", &domain.Ticket{ID: "t1"}, tickets) != nil {
		t.Error("a top-level task has no parent")
	}
}
