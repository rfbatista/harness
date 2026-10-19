package tasks

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

func TestRailGroupsTasksInKanbanOrderWithTheirSessions(t *testing.T) {
	tasks := []*domain.Ticket{
		{ID: "t-done", Title: "Ship it", Status: domain.TicketStatusDone},
		{ID: "t-feed", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
		{ID: "t-todo", Title: "Write docs", Status: domain.TicketStatusTodo},
		{ID: "t-port", Title: "Port tickets", Status: domain.TicketStatusInProgress},
		{ID: "t-old", Title: "Archived", Status: domain.TicketStatusDone, ArchivedAt: &archivedAt}, // off the rail
	}
	sessions := []*domain.Session{
		// One task, several sessions at once: two live, one waiting on you, one over.
		{ID: "s1", TicketID: "t-feed", Status: domain.SessionRunning},
		{ID: "s2", TicketID: "t-feed", Status: domain.SessionIdle},
		{ID: "s3", TicketID: "t-feed", Status: domain.SessionDone},
		{ID: "s4", TicketID: "t-port", Status: domain.SessionThinking},
		{ID: "s5", TicketID: "", Status: domain.SessionRunning}, // no task: not on the rail
	}
	rail := BuildRail("p1", tasks, sessions, "t-port", nil)

	var got []string
	for _, g := range rail.Groups {
		var links []string
		for _, l := range g.Links {
			links = append(links, fmt.Sprintf("%s live=%d attention=%v current=%v", l.Label, l.Live, l.Attention, l.Current))
		}
		got = append(got, fmt.Sprintf("%s (%s, %d): %s", g.Label, g.Key, g.Total, strings.Join(links, ", ")))
	}
	want := []string{
		"In progress (in_progress, 2): Add SSE feed live=2 attention=true current=false, Port tickets live=1 attention=false current=true",
		"Review (review, 0): ",
		"Todo (todo, 1): Write docs live=0 attention=false current=false",
		"Backlog (backlog, 0): ",
		"Done (done, 1): Ship it live=0 attention=false current=false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rail:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if href := rail.Groups[0].Links[0].Href; href != "/projects/p1/tasks/t-feed" {
		t.Errorf("href = %q", href)
	}
	for _, tk := range rail.Seed.Tasks {
		if tk.ID == "t-old" {
			t.Errorf("the seed carries the archived task")
		}
	}
}

func TestRailWithoutTasksSaysSo(t *testing.T) {
	if rail := BuildRail("p1", nil, nil, "", nil); rail.Empty == "" || len(rail.Groups) != 0 {
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
		}, "", nil)
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
	rail := BuildRail("p1", tasks, nil, "t1", nil)
	if rail.Seed == nil || len(rail.Seed.Tasks) != 1 || rail.Seed.Tasks[0].ID != "t1" {
		t.Fatalf("seed = %+v", rail.Seed)
	}
	if rail.Current != "t1" {
		t.Errorf("current = %q", rail.Current)
	}
	empty := BuildRail("p1", nil, nil, "", nil)
	if empty.Seed == nil || empty.Seed.ProjectID != "p1" || len(empty.Seed.Tasks) != 0 || len(empty.Seed.Sessions) != 0 {
		t.Fatalf("empty seed = %+v", empty.Seed)
	}
	if empty.Empty == "" {
		t.Error("an empty rail still says so")
	}
}

// An unknown status still reaches the rail, in a last "Other" group, shown
// only when it has tasks.
func TestRailPutsUnknownStatusesInOtherOnlyWhenThereAreSome(t *testing.T) {
	rail := BuildRail("p1", []*domain.Ticket{{ID: "t1", Title: "Odd", Status: "blocked"}}, nil, "", nil)
	last := rail.Groups[len(rail.Groups)-1]
	if len(rail.Groups) != 6 || last.Key != "other" || last.Label != "Other" || last.Total != 1 {
		t.Fatalf("groups = %+v", rail.Groups)
	}
	if rail := BuildRail("p1", []*domain.Ticket{{ID: "t1", Status: domain.TicketStatusTodo}}, nil, "", nil); len(rail.Groups) != 5 {
		t.Errorf("no Other without unknown statuses: %d groups", len(rail.Groups))
	}
}

// Done shows its five most recently updated tasks; the rest wait behind
// "Show N more". The open task and the tasks that need the person stay shown
// whatever their age.
func TestRailCapsDoneAtItsFiveLatestKeepingTheOpenTaskAndWhatNeedsYou(t *testing.T) {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var tasks []*domain.Ticket
	for i := range 9 { // d0 is the oldest, d8 the newest
		tasks = append(tasks, &domain.Ticket{ID: fmt.Sprintf("d%d", i), Title: fmt.Sprintf("Done %d", i), Status: domain.TicketStatusDone, UpdatedAt: base.Add(time.Duration(i) * time.Hour)})
	}
	tasks[2].PendingReviews = 1 // old, but waits on the person
	rail := BuildRail("p1", tasks, nil, "d0", nil)
	done := rail.Groups[4]
	ids := func(links []shell.Link) string {
		var out []string
		for _, l := range links {
			out = append(out, l.TaskID)
		}
		return strings.Join(out, " ")
	}
	if done.Key != "done" || done.Total != 9 {
		t.Fatalf("done = %+v", done)
	}
	if got := ids(done.Links); got != "d8 d7 d6 d5 d4 d2 d0" {
		t.Errorf("shown = %s", got)
	}
	if got := ids(done.More); got != "d3 d1" {
		t.Errorf("more = %s", got)
	}
}

// Only Done is capped; the other groups keep creation order in full.
func TestRailDoesNotCapTheActiveGroups(t *testing.T) {
	var tasks []*domain.Ticket
	for i := range 8 {
		tasks = append(tasks, &domain.Ticket{ID: fmt.Sprintf("b%d", i), Status: domain.TicketStatusBacklog})
	}
	backlog := BuildRail("p1", tasks, nil, "", nil).Groups[3]
	if len(backlog.Links) != 8 || len(backlog.More) != 0 || backlog.Links[0].TaskID != "b0" {
		t.Fatalf("backlog = %+v", backlog)
	}
}

// The person's collapsed groups come from the rail_collapsed cookie; an empty
// group has nothing to fold, and keys no group has are ignored.
func TestRailPaintsTheCollapsedGroups(t *testing.T) {
	tasks := []*domain.Ticket{
		{ID: "t1", Status: domain.TicketStatusBacklog},
		{ID: "t2", Status: domain.TicketStatusDone},
		{ID: "t3", Status: domain.TicketStatusTodo},
	}
	rail := BuildRail("p1", tasks, nil, "", map[string]bool{"backlog": true, "review": true, "done": true, "bogus": true})
	var got []string
	for _, g := range rail.Groups {
		if g.Collapsed {
			got = append(got, g.Key)
		}
	}
	if strings.Join(got, ",") != "backlog,done" {
		t.Fatalf("collapsed = %v", got)
	}
}

func TestRailCountsWhatNeedsThePerson(t *testing.T) {
	rail := BuildRail("p1",
		[]*domain.Ticket{
			{ID: "t1", Status: domain.TicketStatusTodo, PendingReviews: 2},
			{ID: "t2", Status: domain.TicketStatusInProgress},
			{ID: "t3", Status: domain.TicketStatusDone},
		},
		[]*domain.Session{{ID: "s1", TicketID: "t2", Status: domain.SessionIdle}}, "", nil)
	if n := rail.NeedsYou(); n != 2 {
		t.Errorf("needs you = %d", n)
	}
}
