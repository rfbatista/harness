package tasks

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestRailWithoutTasksSaysSo(t *testing.T) {
	if rail := BuildRail("p1", nil, nil, ""); rail.Empty == "" {
		t.Fatalf("rail = %+v", rail)
	}
}

// The rail carries what the browser needs to keep it live: the project's
// sessions on tasks, which are the only sessions the rail counts.
func TestRailSeedsTheLiveRail(t *testing.T) {
	rail := BuildRail("p1",
		[]*domain.Ticket{{ID: "t1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}},
		[]*domain.Session{
			{ID: "s1", TicketID: "t1", Status: domain.SessionWaitingApproval, PendingApprovals: 1},
			{ID: "loose", Status: domain.SessionRunning},
		}, "")
	if rail.Seed == nil || rail.Seed.ProjectID != "p1" || len(rail.Seed.Sessions) != 1 {
		t.Fatalf("seed = %+v", rail.Seed)
	}
	if s := rail.Seed.Sessions[0]; s.ID != "s1" || s.TicketID != "t1" || s.Status != "waiting_approval" || s.PendingApprovals != 1 {
		t.Fatalf("seed session = %+v", s)
	}
}

// The seed also carries the project's tasks, so the browser can group the
// rail and draw the board without another request; and it is there even
// with no tasks, so a task created by an agent shows up on an empty project.
func TestRailSeedCarriesTheTasksEvenWhenThereAreNone(t *testing.T) {
	tasks := []*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}
	rail := BuildRail("p1", tasks, nil, "t1")
	if rail.Seed == nil || len(rail.Seed.Tasks) != 1 || rail.Seed.Tasks[0].ID != "t1" {
		t.Fatalf("seed = %+v", rail.Seed)
	}
	if rail.Current != "t1" || rail.Empty != "" {
		t.Errorf("current = %q, empty = %q", rail.Current, rail.Empty)
	}
	empty := BuildRail("p1", nil, nil, "")
	if empty.Seed == nil || empty.Seed.ProjectID != "p1" || len(empty.Seed.Tasks) != 0 || len(empty.Seed.Sessions) != 0 {
		t.Fatalf("empty seed = %+v", empty.Seed)
	}
	if empty.Empty == "" {
		t.Error("an empty rail still says so")
	}
}

// Archived tasks are not on the rail, but a child archived on its own while its
// feature is on the board is in the seed: the feature's card lists it.
func TestRailSeedLeavesOutArchivedTasksExceptAChildOfALiveFeature(t *testing.T) {
	archived := func(id, parent string) *domain.Ticket {
		at := now()
		return &domain.Ticket{ID: id, ProjectID: "p1", ParentID: parent, Status: domain.TicketStatusDone, ArchivedAt: &at}
	}
	rail := BuildRail("p1", []*domain.Ticket{
		{ID: "f", ProjectID: "p1", Status: domain.TicketStatusInProgress},
		archived("kid", "f"),
		archived("old", ""),
	}, nil, "")
	var ids []string
	for _, task := range rail.Seed.Tasks {
		ids = append(ids, task.ID)
	}
	if len(ids) != 2 || ids[0] != "f" || ids[1] != "kid" {
		t.Errorf("seed tasks = %v, want [f kid]", ids)
	}
}
