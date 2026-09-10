package rollup

import (
	"testing"

	"operators-mcp/internal/domain"
)

func sess(id, ticket string, st domain.SessionStatus, cost float64) *domain.Session {
	return &domain.Session{ID: id, TicketID: ticket, Status: st, CostUSD: cost, RepositoryID: "repo-" + id}
}

func TestAgentStatusMirrorsFlutterMapper(t *testing.T) {
	cases := map[domain.SessionStatus]AgentStatus{
		domain.SessionRunning:         Run,
		domain.SessionIdle:            Run,
		domain.SessionThinking:        Think,
		domain.SessionStarting:        Think,
		domain.SessionWaitingApproval: Review,
		domain.SessionPaused:          Paused,
		domain.SessionDone:            Done,
		domain.SessionStopped:         Done,
		domain.SessionFailed:          Block,
		domain.SessionStatus("weird"): Think,
	}
	for in, want := range cases {
		if got := AgentStatusOf(in); got != want {
			t.Errorf("%s: got %v want %v", in, got, want)
		}
	}
}

func TestTaskStatusPrecedence(t *testing.T) {
	cases := []struct {
		name string
		in   []*domain.Session
		want TaskStatus
	}{
		{"empty", nil, Empty},
		{"blocked beats review", []*domain.Session{sess("a", "t", domain.SessionWaitingApproval, 0), sess("b", "t", domain.SessionFailed, 0)}, Blocked},
		{"review beats running", []*domain.Session{sess("a", "t", domain.SessionRunning, 0), sess("b", "t", domain.SessionWaitingApproval, 0)}, NeedsReview},
		{"running beats paused", []*domain.Session{sess("a", "t", domain.SessionPaused, 0), sess("b", "t", domain.SessionThinking, 0)}, Running},
		{"paused beats done", []*domain.Session{sess("a", "t", domain.SessionDone, 0), sess("b", "t", domain.SessionPaused, 0)}, PausedStatus},
		{"all done", []*domain.Session{sess("a", "t", domain.SessionStopped, 0)}, DoneStatus},
	}
	for _, c := range cases {
		if got := Fold(c.in).Status; got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestFoldCountsAndCost(t *testing.T) {
	r := Fold([]*domain.Session{
		sess("a", "t", domain.SessionRunning, 1.5),
		sess("b", "t", domain.SessionWaitingApproval, 0.25),
		sess("c", "t", domain.SessionFailed, 0),
		sess("d", "t", domain.SessionDone, 2),
	})
	if r.RunningCount != 1 || r.ReviewCount != 1 || r.BlockedCount != 1 || r.DoneCount != 1 {
		t.Fatalf("counts: %+v", r)
	}
	if r.CostUSD != 3.75 || len(r.Sessions) != 4 {
		t.Fatalf("cost/sessions: %+v", r)
	}
}

func TestKanbanColumnPutsEveryStatusInOneColumn(t *testing.T) {
	want := map[TaskStatus]Column{
		Empty: InFlight, Running: InFlight, PausedStatus: InFlight,
		NeedsReview: ReviewColumn, Blocked: BlockedColumn, DoneStatus: DoneColumn,
	}
	for st, col := range want {
		if got := ColumnFor(st); got != col {
			t.Errorf("%v: got %v want %v", st, got, col)
		}
	}
}

func TestJoinFiltersByProjectAndStatus(t *testing.T) {
	tickets := []*domain.Ticket{
		{ID: "t1", ProjectID: "p1", Title: "one"},
		{ID: "t2", ProjectID: "p1", Title: "two"},
		{ID: "t3", ProjectID: "p2", Title: "three"},
	}
	sessions := []*domain.Session{
		sess("a", "t1", domain.SessionWaitingApproval, 0),
		sess("b", "t2", domain.SessionRunning, 0),
		sess("c", "t3", domain.SessionFailed, 0),
		sess("d", "", domain.SessionRunning, 0), // unlinked
	}
	all := Join(tickets, sessions, "", FilterAll)
	if len(all) != 3 {
		t.Fatalf("all: want 3 rows got %d", len(all))
	}
	attention := Join(tickets, sessions, "", FilterAttention)
	if len(attention) != 2 || attention[0].Ticket.ID != "t1" || attention[1].Ticket.ID != "t3" {
		t.Fatalf("attention: %+v", ids(attention))
	}
	p1inflight := Join(tickets, sessions, "p1", FilterInFlight)
	if len(p1inflight) != 1 || p1inflight[0].Ticket.ID != "t2" {
		t.Fatalf("p1 inflight: %+v", ids(p1inflight))
	}
	if got := Join(tickets, sessions, "", FilterDone); len(got) != 0 {
		t.Fatalf("done: want none got %+v", ids(got))
	}
}

func TestStatsCountAcrossSessions(t *testing.T) {
	tickets := []*domain.Ticket{{ID: "t1", ProjectID: "p1"}, {ID: "t2", ProjectID: "p1"}}
	sessions := []*domain.Session{
		sess("a", "t1", domain.SessionRunning, 1),
		sess("b", "t1", domain.SessionThinking, 1),
		sess("c", "t2", domain.SessionWaitingApproval, 0.5),
		sess("d", "", domain.SessionFailed, 0),
	}
	s := Stats(Join(tickets, sessions, "", FilterAll), sessions)
	if s.TasksInFlight != 1 || s.AgentsWorking != 2 || s.NeedsReview != 1 || s.Blocked != 1 || s.SpendUSD != 2.5 {
		t.Fatalf("stats: %+v", s)
	}
}

func ids(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Ticket.ID)
	}
	return out
}
