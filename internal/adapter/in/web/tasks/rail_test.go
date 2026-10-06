package tasks

import (
	"fmt"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func TestRailGroupsTasksInKanbanOrderWithTheirSessions(t *testing.T) {
	tasks := []*domain.Ticket{
		{ID: "t-done", Title: "Ship it", Status: domain.TicketStatusDone},
		{ID: "t-feed", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
		{ID: "t-todo", Title: "Write docs", Status: domain.TicketStatusTodo},
		{ID: "t-port", Title: "Port tickets", Status: domain.TicketStatusInProgress},
	}
	sessions := []*domain.Session{
		// One task, several sessions at once: two live, one waiting on you, one over.
		{ID: "s1", TicketID: "t-feed", Status: domain.SessionRunning},
		{ID: "s2", TicketID: "t-feed", Status: domain.SessionIdle},
		{ID: "s3", TicketID: "t-feed", Status: domain.SessionDone},
		{ID: "s4", TicketID: "t-port", Status: domain.SessionThinking},
		{ID: "s5", TicketID: "", Status: domain.SessionRunning}, // no task: not on the rail
	}
	rail := BuildRail("p1", tasks, sessions, "t-port")

	var got []string
	for _, g := range rail.Groups {
		var links []string
		for _, l := range g.Links {
			links = append(links, fmt.Sprintf("%s live=%d attention=%v current=%v", l.Label, l.Live, l.Attention, l.Current))
		}
		got = append(got, g.Label+": "+strings.Join(links, ", "))
	}
	want := []string{
		"in progress: Add SSE feed live=2 attention=true current=false, Port tickets live=1 attention=false current=true",
		"todo: Write docs live=0 attention=false current=false",
		"done: Ship it live=0 attention=false current=false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rail:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if href := rail.Groups[0].Links[0].Href; href != "/projects/p1/tasks/t-feed" {
		t.Errorf("href = %q", href)
	}
}

func TestRailWithoutTasksSaysSo(t *testing.T) {
	if rail := BuildRail("p1", nil, nil, ""); rail.Empty == "" || len(rail.Groups) != 0 {
		t.Fatalf("rail = %+v", rail)
	}
}

// The rail carries what the browser needs to keep it live: each link's task,
// and the project's sessions on tasks.
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
	if id := rail.Groups[0].Links[0].TaskID; id != "t1" {
		t.Errorf("task id = %q", id)
	}
}

// The seed also carries the project's tasks, so the browser can regroup the
// rail and draw the board without another request; and it is there even
// with no tasks, so a task created by an agent shows up on an empty project.
func TestRailSeedCarriesTheTasksEvenWhenThereAreNone(t *testing.T) {
	tasks := []*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}
	rail := BuildRail("p1", tasks, nil, "t1")
	if rail.Seed == nil || len(rail.Seed.Tasks) != 1 || rail.Seed.Tasks[0].ID != "t1" {
		t.Fatalf("seed = %+v", rail.Seed)
	}
	if rail.Current != "t1" {
		t.Errorf("current = %q", rail.Current)
	}
	empty := BuildRail("p1", nil, nil, "")
	if empty.Seed == nil || empty.Seed.ProjectID != "p1" || len(empty.Seed.Tasks) != 0 || len(empty.Seed.Sessions) != 0 {
		t.Fatalf("empty seed = %+v", empty.Seed)
	}
	if empty.Empty == "" {
		t.Error("an empty rail still says so")
	}
}
