package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// architectWorld is board() with an architect on t-feed: it delegated s-server,
// which delegated s-tests; s-peer was started by a person.
func architectWorld() world {
	w := board()
	arch := "s-arch"
	w.tickets[0].ArchitectSessionID = &arch
	w.sessions = append(w.sessions,
		&domain.Session{ID: "s-arch", ProjectID: "p1", TicketID: "t-feed", Task: "shape the work", Mode: domain.SessionModeArchitect, Role: domain.RoleArchitect, ArchitectSessionID: &arch, Status: domain.SessionRunning, UpdatedAt: now.Add(-time.Hour)},
		&domain.Session{ID: "s-server", ProjectID: "p1", TicketID: "t-feed", Task: "build the server", ParentSessionID: "s-arch", Role: domain.RoleDelegate, ArchitectSessionID: &arch, Status: domain.SessionRunning, UpdatedAt: now.Add(-30 * time.Minute)},
		&domain.Session{ID: "s-tests", ProjectID: "p1", TicketID: "t-feed", Task: "test the server", ParentSessionID: "s-server", Role: domain.RoleDelegate, ArchitectSessionID: &arch, Status: domain.SessionRunning, UpdatedAt: now.Add(-20 * time.Minute)},
	)
	return w
}

func TestTaskPageLeadsWithTheArchitectAndItsDelegates(t *testing.T) {
	rec := get(t, newTestHandler(t, architectWorld()), "/projects/p1/tasks/t-feed")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	ssr := sessionsFirstPaint(body)
	arch := strings.Index(ssr, `data-role="architect"`)
	server := strings.Index(ssr, `data-depth="1" data-role="delegate"`)
	tests := strings.Index(ssr, `data-depth="2" data-role="delegate"`)
	turn := strings.Index(ssr, "implement this task") // a peer that needs you
	if arch < 0 || server < arch || tests < server || turn < tests {
		t.Errorf("first paint order: architect %d, delegate %d, its delegate %d, then the peers %d", arch, server, tests, turn)
	}
	for _, want := range []string{
		`<span class="[ badge ]">architect</span>`,
		`Architect`,
		`x-bind:data-role="row.roleAttr"`,
		`"role":"architect"`,
		`"architect_session_id":"s-arch"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task page is missing %q", want)
		}
	}
}

func TestTaskPageWithoutAnArchitectHasNoRoles(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String()
	ssr := sessionsFirstPaint(body)
	for _, absent := range []string{"data-role", "data-depth", ">architect<", ">Architect"} {
		if strings.Contains(ssr, absent) {
			t.Errorf("a task without an architect shows %q", absent)
		}
	}
}

// sessionsFirstPaint is the server-rendered copy of the task's session list
// (the rail has its own).
func sessionsFirstPaint(body string) string {
	list := body[strings.Index(body, `aria-label="Sessions of this task"`):]
	return list[:strings.Index(list, `x-for="group in groups"`)]
}
