package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// fakeArchived lists a project's archived tasks as the planning service does:
// newest archived first.
type fakeArchived []*domain.Ticket

func (f fakeArchived) ListArchivedTickets(_ context.Context, projectID string) ([]*domain.Ticket, error) {
	var out []*domain.Ticket
	for _, t := range f {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}

func TestArchivedViewListsTheArchivedTasksWithRestoreAndDelete(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	deps := boardDeps()
	deps.ArchivedList = fakeArchived{
		{ID: "t-old", ProjectID: "p1", Title: "Old work", Status: domain.TicketStatusDone, ArchivedAt: &at, StatusSince: at.Add(-24 * time.Hour),
			MergeState: &domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1}},
	}
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, NewHandler(deps, assets, nil), "/projects/p1/archived")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`x-data="tasksArchived"`,
		`<script id="archived-seed" type="application/json">`,
		`"id":"t-old"`,
		`data-archived-list`,
		`data-task-id="t-old"`,
		`href="/projects/p1/tasks/t-old">Old work</a>`,
		"1 archived task. Restore puts a task back in Done.",
		">Restore</button>",
		">Delete</button>",
		`data-merge-state="merged"`,
		`href="/projects/p1">Back to the board</a>`,
		`data-confirm="delete"`,
		">Delete forever</button>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("archived view lacks %q", want)
		}
	}
}

func TestArchivedViewWithNothingArchivedSaysWhereTasksComeFrom(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	body := get(t, NewHandler(boardDeps(), assets, nil), "/projects/p1/archived").Body.String()
	if !strings.Contains(body, "No archived tasks.") {
		t.Errorf("the empty archived view does not say so")
	}
}
