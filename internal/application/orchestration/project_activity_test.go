package orchestration

import (
	"context"
	"reflect"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

func TestLiveProjectSessions_NamesLiveSessionsWithTheirAgent(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewSessionRepository(db)
	res := &fakeResolver{agents: map[string]*domain.Agent{"a1": {ID: "a1", Name: "go-developer"}}}
	svc := &Service{sessions: repo, catalog: catalogOf(res)}

	for _, s := range []*domain.Session{
		{ID: "named", ProjectID: "p1", TicketID: "tk1", AgentID: "a1", Status: domain.SessionRunning},
		{ID: "deleted-agent", ProjectID: "p1", AgentID: "gone", Status: domain.SessionPaused},
		{ID: "no-agent", ProjectID: "p1", Status: domain.SessionWaitingApproval},
		{ID: "finished", ProjectID: "p1", AgentID: "a1", Status: domain.SessionDone},
		{ID: "elsewhere", ProjectID: "p2", Status: domain.SessionRunning},
	} {
		s.Task = "t"
		if _, err := repo.Create(s); err != nil {
			t.Fatal(err)
		}
	}

	got, err := svc.LiveProjectSessions(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.ProjectSessionRef{}
	for _, r := range got {
		byID[r.ID] = r
	}
	want := map[string]domain.ProjectSessionRef{
		"named":         {ID: "named", TicketID: "tk1", Agent: "go-developer"},
		"deleted-agent": {ID: "deleted-agent", Agent: "gone"},
		"no-agent":      {ID: "no-agent"},
	}
	if !reflect.DeepEqual(byID, want) {
		t.Fatalf("LiveProjectSessions = %+v, want %+v", byID, want)
	}

	none, err := svc.LiveProjectSessions(context.Background(), "p3")
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("no sessions = %#v, %v; want empty", none, err)
	}
}

// Every status is either live or terminal, so a new status cannot slip past
// the delete guard unnoticed.
func TestLiveStatuses_AreTheNonTerminalOnes(t *testing.T) {
	all := []domain.SessionStatus{
		domain.SessionStarting, domain.SessionRunning, domain.SessionIdle, domain.SessionThinking,
		domain.SessionWaitingApproval, domain.SessionPaused, domain.SessionDone, domain.SessionFailed, domain.SessionStopped,
	}
	live := map[domain.SessionStatus]bool{}
	for _, s := range liveStatuses {
		live[s] = true
	}
	for _, s := range all {
		if live[s] == s.IsTerminal() {
			t.Errorf("%s: live=%v terminal=%v", s, live[s], s.IsTerminal())
		}
	}
}
