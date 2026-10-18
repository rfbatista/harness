package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// countingTickets and countingSessions count the list reads a page makes.
type countingTickets struct {
	fakeTickets
	lists *int
}

func (c countingTickets) ListTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error) {
	*c.lists++
	return c.fakeTickets.ListTickets(ctx, projectID)
}

type countingSessions struct {
	fakeSessions
	lists *int
}

func (c countingSessions) List(ctx context.Context, f ports.SessionFilter) ([]*domain.Session, error) {
	*c.lists++
	return c.fakeSessions.List(ctx, f)
}

// A page and its layout (the rail, the board counts) read the project's
// tickets and sessions once per request, and the task page still lists only
// its own task's sessions.
func TestAPageAndItsLayoutReadEachListOnce(t *testing.T) {
	w := board()
	var tickets, sessions int
	h := NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: countingTickets{fakeTickets{w.tickets}, &tickets},
		Sessions: countingSessions{fakeSessions{w.sessions}, &sessions}, Agents: fakeAgents{w.agents},
		Repositories: fakeRepos{w.repos}, EnvFiles: w.env, Now: func() time.Time { return now },
	}, builtAssets(t), nil)

	for _, path := range []string{"/projects/p1", "/projects/p1/tasks/t-feed"} {
		tickets, sessions = 0, 0
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d\n%s", path, rec.Code, rec.Body.String())
		}
		if tickets != 1 || sessions != 1 {
			t.Errorf("GET %s read the tickets %d times and the sessions %d times, want once each", path, tickets, sessions)
		}
	}

	body := get(t, h, "/projects/p1/tasks/t-docs").Body.String()
	_, seed, _ := strings.Cut(body, `<script id="sessions-seed" type="application/json">`)
	seed, _, _ = strings.Cut(seed, "</script>")
	if !strings.Contains(seed, `"id":"s3"`) {
		t.Errorf("the t-docs page does not seed its session s3:\n%s", seed)
	}
	for _, s := range w.sessions {
		if s.TicketID != "t-docs" && strings.Contains(seed, `"id":"`+s.ID+`"`) {
			t.Errorf("the t-docs page seeds session %s of task %s", s.ID, s.TicketID)
		}
	}
}
