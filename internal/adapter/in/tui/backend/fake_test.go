package backend

import (
	"testing"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

var _ Backend = (*Fake)(nil)

func TestFakeListTicketsFiltersByProject(t *testing.T) {
	f := NewFake()
	f.Tickets = []*domain.Ticket{{ID: "a", ProjectID: "p1"}, {ID: "b", ProjectID: "p2"}}
	if got := f.ListTickets("p1"); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("p1: %+v", got)
	}
	if got := f.ListTickets(""); len(got) != 2 {
		t.Fatalf("all projects: want 2 got %d", len(got))
	}
}

func TestFakeListSessionsHonoursFilter(t *testing.T) {
	f := NewFake()
	f.Sessions = []*domain.Session{
		{ID: "s1", ProjectID: "p1", TicketID: "t1", Status: domain.SessionRunning},
		{ID: "s2", ProjectID: "p1", TicketID: "t2", Status: domain.SessionDone},
		{ID: "s3", ProjectID: "p2", TicketID: "t1", Status: domain.SessionRunning},
	}
	got := f.ListSessions(ports.SessionFilter{ProjectID: "p1", Statuses: []domain.SessionStatus{domain.SessionRunning, domain.SessionIdle}})
	if len(got) != 1 || got[0].ID != "s1" {
		t.Fatalf("filtered: %+v", got)
	}
	if got := f.ListSessions(ports.SessionFilter{TicketID: "t1"}); len(got) != 2 {
		t.Fatalf("by ticket: want 2 got %d", len(got))
	}
}

func TestFakeListRepositoriesFiltersByProject(t *testing.T) {
	f := NewFake()
	f.Repositories = []*domain.Repository{{ID: "r1", ProjectID: "p1"}, {ID: "r2", ProjectID: "p2"}}
	if got := f.ListRepositories("p2"); len(got) != 1 || got[0].ID != "r2" {
		t.Fatalf("p2: %+v", got)
	}
}

func TestFakeSettingsRoundTrip(t *testing.T) {
	f := NewFake()
	f.Settings[domain.SettingWorkspacesRoot] = "/tmp/wt"
	got, err := f.GetSettings()
	if err != nil || got[domain.SettingWorkspacesRoot] != "/tmp/wt" {
		t.Fatalf("settings: %v %v", got, err)
	}
}
