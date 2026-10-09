package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// The rail's first paint (the server copy the live rail takes over): the
// skip link, New task, the Project nav with the open page marked, and the
// Tasks nav's five groups with their counts.

func getWithCookie(t *testing.T, h http.Handler, path, cookie string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Cookie", cookie)
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", path, rec.Code)
	}
	return rec.Body.String()
}

// currentProjectPage is the Project nav's link marked aria-current, "" when none.
func currentProjectPage(body string) string {
	nav := body[strings.Index(body, `aria-label="Project"`):]
	nav = nav[:strings.Index(nav, "</nav>")]
	m := regexp.MustCompile(`<a href="([^"]+)" aria-current="page">`).FindStringSubmatch(nav)
	if m == nil {
		return ""
	}
	return m[1]
}

func TestRailNamesTheProjectsPagesAndMarksTheOpenOne(t *testing.T) {
	w := board()
	h := newTestHandler(t, w)
	body := get(t, h, "/projects/p1").Body.String()
	for _, want := range []string{
		`<a class="[ skip-link ] [ button ]" data-size="sm" href="#main">Skip to content</a>`,
		`<main id="main"`,
		`<nav class="[ nav ]" aria-label="Project"><ul role="list">`,
		`<span class="[ label ]">Board</span>`,
		`<span class="[ label ]">Review requests</span>`,
		`<span class="[ label ]">Design assets</span>`,
		`<nav class="[ nav ]" aria-label="Tasks">`,
		`<h2 class="[ title ]">Tasks <span class="[ count ]">3</span></h2>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("project page is missing %q", want)
		}
	}
	if strings.Contains(body, ">Reviews<") {
		t.Error(`the destination is "Review requests" now`)
	}
	for path, want := range map[string]string{
		"/projects/p1":              "/projects/p1",
		"/projects/p1/reviews":      "/projects/p1/reviews",
		"/projects/p1/design":       "/projects/p1/design",
		"/projects/p1/tasks/t-feed": "",
		"/projects/p1/tasks/new":    "",
	} {
		if got := currentProjectPage(get(t, h, path).Body.String()); got != want {
			t.Errorf("%s: current project page = %q, want %q", path, got, want)
		}
	}
}

func TestRailGroupsAreHeadingsWithCountsAndTheRowsCarryOneMetaItem(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1").Body.String()
	for _, want := range []string{
		// Every group, empty ones included, as an h3 with its count in words.
		`<section class="[ group ]" aria-labelledby="rail-in_progress-heading" data-key="in_progress"><h3 id="rail-in_progress-heading">`,
		`<span class="[ name ]">Review</span> <span class="[ count ]">0<span class="[ visually-hidden ]"> tasks</span></span></h3></section>`,
		`data-empty="true"`,
		`<span class="[ count ]">1<span class="[ visually-hidden ]"> task, 1 needs you</span></span>`,
		// The feed task waits on the person: the amber wash, "waiting".
		`data-task-id="t-feed" data-attention="true"`,
		`<span class="[ meta ]" data-tone="attention">waiting</span> <span class="[ visually-hidden ]"> on you</span>`,
		// The filter's place, held so the handover does not move the list.
		`<div class="[ filter-placeholder ]"></div>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("project page is missing %q", want)
		}
	}
}

func TestRailPaintsTheGroupsTheCookieFolds(t *testing.T) {
	h := newTestHandler(t, board())
	body := getWithCookie(t, h, "/projects/p1", shell.CollapsedCookie+"=todo,review")
	if !strings.Contains(body, `data-key="todo" data-collapsed="true"`) {
		t.Error("todo is not painted folded")
	}
	if strings.Contains(body, `data-key="review" data-collapsed`) {
		t.Error("an empty group has nothing to fold")
	}
	for _, cookie := range []string{"", shell.CollapsedCookie + "=Todo"} {
		if b := getWithCookie(t, h, "/projects/p1", cookie); strings.Contains(b, `data-collapsed="true"`) {
			t.Errorf("cookie %q folds a group", cookie)
		}
	}
}

func TestRailCapsDoneBehindShowMore(t *testing.T) {
	w := board()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range 8 {
		w.tickets = append(w.tickets, &domain.Ticket{ID: fmt.Sprintf("d%d", i), ProjectID: "p1", Title: fmt.Sprintf("Finished %d", i), Status: domain.TicketStatusDone, UpdatedAt: base.Add(time.Duration(i) * time.Hour)})
	}
	body := get(t, newTestHandler(t, w), "/projects/p1").Body.String()
	// Nine done: five shown, four behind the summary.
	if !strings.Contains(body, `<summary><span class="[ closed ]">Show 4 more<span class="[ visually-hidden ]"> done tasks</span></span><span class="[ opened ]">Show fewer</span></summary>`) {
		t.Error("Done is not capped behind Show 4 more")
	}
	// Newest first: d7…d3 shown, d2, d1, d0 and the undated t-ship behind it.
	if i, j := strings.Index(body, ">Finished 7<"), strings.Index(body, ">Finished 1<"); i < 0 || j < 0 || i > j || j < strings.Index(body, "Show 4 more") || strings.Index(body, ">Finished 3<") > strings.Index(body, "Show 4 more") {
		t.Error("Done is not newest first, with the oldest behind the summary")
	}
}

func TestThePhoneTasksButtonIsALinkToTheBoardWithWhatNeedsYou(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-docs").Body.String()
	for _, want := range []string{
		`class="[ button ] [ tasks-toggle ]" data-variant="ghost" href="/projects/p1" aria-controls="rail"`,
		`<span x-text="needsYouCount">1</span>`,
		`<div class="[ drawer-head ]"><span class="[ truncate ]">coding_pool</span>`,
		`<div class="[ scrim ]" x-data="tasksRailToggle"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task page is missing %q", want)
		}
	}
}

func TestRailWithoutAProjectPointsAtThePicker(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/new").Body.String()
	if !strings.Contains(body, `<p class="[ note ]">Choose a project in the picker above to see its tasks.</p>`) {
		t.Error("no hint to pick a project")
	}
	for _, absent := range []string{`aria-label="Tasks"`, `aria-label="Project"`, "tasks-toggle"} {
		if strings.Contains(body, absent) {
			t.Errorf("a rail without a project shows %q", absent)
		}
	}
}

func TestEveryAppShellPageHasTheSkipLinksTarget(t *testing.T) {
	h := newTestHandler(t, board())
	for _, path := range []string{"/projects/p1", "/projects/p1/tasks/t-feed", "/projects/p1/tasks/new", "/projects/p1/reviews", "/projects/p1/design", "/projects/new", "/projects/p1/settings"} {
		body := get(t, h, path).Body.String()
		if strings.Contains(body, `href="#main"`) && !strings.Contains(body, `<main id="main"`) {
			t.Errorf("%s: the skip link has no target", path)
		}
	}
}
