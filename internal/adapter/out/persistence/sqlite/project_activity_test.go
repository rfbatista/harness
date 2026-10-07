package sqlite

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestRepositoryRepo_CountByProject(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRepositoryRepository(db)
	for _, pid := range []string{"p1", "p1", "p2"} {
		if _, err := r.Create(pid, "r", "", "https://example.com/r", "/tmp/r"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.CountByProject()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["p1"] != 2 || got["p2"] != 1 {
		t.Fatalf("CountByProject = %v, want p1:2 p2:1", got)
	}
}

func TestTicketRepo_ActivityByProject(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTicketRepository(db)
	var newest *domain.Ticket
	for _, s := range []domain.TicketStatus{domain.TicketStatusBacklog, domain.TicketStatusReview, domain.TicketStatusDone} {
		if newest, err = r.Create("p1", "t", "", s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Create("p2", "t", "", domain.TicketStatusDone); err != nil {
		t.Fatal(err)
	}
	got, err := r.ActivityByProject()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["p1"].OpenCount != 2 || got["p2"].OpenCount != 0 {
		t.Fatalf("ActivityByProject = %+v, want p1 2 open, p2 0 open", got)
	}
	if !got["p1"].LastUpdatedAt.Equal(r.Get(newest.ID).UpdatedAt) {
		t.Fatalf("p1 last update = %v, want %v", got["p1"].LastUpdatedAt, newest.UpdatedAt)
	}
}

func TestSessionRepo_ActivityByProject(t *testing.T) {
	r := newTestDB(t)
	for _, s := range []struct {
		project string
		status  domain.SessionStatus
	}{
		{"p1", domain.SessionRunning}, {"p1", domain.SessionPaused}, {"p1", domain.SessionWaitingApproval},
		{"p1", domain.SessionDone}, {"p1", domain.SessionFailed}, {"p1", domain.SessionStopped},
		{"p2", domain.SessionDone},
	} {
		if _, err := r.Create(&domain.Session{ProjectID: s.project, Task: "t", Status: s.status}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.ActivityByProject()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["p1"].LiveCount != 3 || got["p2"].LiveCount != 0 || got["p2"].LastActivityAt.IsZero() {
		t.Fatalf("ActivityByProject = %+v, want p1 3 live, p2 0 live with activity", got)
	}
}
