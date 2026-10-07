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

func TestTaskPagePaintsTheReviewsWaitingOnThePerson(t *testing.T) {
	w := architectWorld()
	w.tickets[0].PendingReviews = 2
	body := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed").Body.String()
	band := body[strings.Index(body, `aria-label="Review requests"`):]
	band = band[:strings.Index(band, ">")]
	for _, want := range []string{`x-data="reviewsInbox"`, `data-ticket-id="t-feed"`, `data-pending="2"`, ` data-attention`} {
		if !strings.Contains(band, want) {
			t.Errorf("the band %q lacks %s", band, want)
		}
	}
	if strings.Contains(band, " hidden") {
		t.Errorf("a band with reviews waiting is hidden: %q", band)
	}
	for _, want := range []string{
		"2 reviews wait on you",
		`<script id="reviews-seed" type="application/json">`,
		`"s-server":"plain claude · build the server"`,
		`"t-feed":{"title":"Add SSE feed","href":"/projects/p1/tasks/t-feed"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task page is missing %q", want)
		}
	}
}

func TestTaskPageWithNoReviewsPaintsTheBandHidden(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String()
	band := body[strings.Index(body, `aria-label="Review requests"`):]
	band = band[:strings.Index(band, ">")]
	if !strings.Contains(band, " hidden") || strings.Contains(band, " data-attention") {
		t.Errorf("a band with nothing waiting should be painted hidden and quiet: %q", band)
	}
	if strings.Contains(body, "wait on you") {
		t.Error("nothing waits, yet the page says something does")
	}
}
