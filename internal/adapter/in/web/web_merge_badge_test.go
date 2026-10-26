package web

import (
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// The task card's merge badge shows the merge state the server computed, as it
// is: a task whose session was squashed into the default branch arrives as
// merged and the board says so, without deciding anything itself.
func TestTheBoardShowsTheServersMergeStateForASquashedTask(t *testing.T) {
	deps := boardDeps()
	deps.Tasks = fakeTickets{[]*domain.Ticket{
		{ID: "t-squashed", ProjectID: "p1", Title: "Squashed into main", Status: domain.TicketStatusReview,
			MergeState: &domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1}},
		{ID: "t-open", ProjectID: "p1", Title: "Squash into a feature branch", Status: domain.TicketStatusReview,
			MergeState: &domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1}},
	}}
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, NewHandler(deps, assets, nil), "/projects/p1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-merge-state="merged"`, `data-merge-state="not_merged"`} {
		if strings.Count(body, want) != 1 {
			t.Errorf("want one %s in the board, got %d", want, strings.Count(body, want))
		}
	}
}
