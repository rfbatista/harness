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

// tagAt is the opening tag that contains marker: from its "<" to its ">".
func tagAt(body, marker string) string {
	at := strings.Index(body, marker)
	if at < 0 {
		return ""
	}
	return body[strings.LastIndex(body[:at], "<") : at+strings.Index(body[at:], ">")+1]
}

func TestTaskPagePaintsTheReviewsWaitingOnThePerson(t *testing.T) {
	w := architectWorld()
	w.tickets[0].PendingReviews = 2
	body := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed").Body.String()
	holder := tagAt(body, `x-data="reviewsInbox"`)
	for _, want := range []string{`data-ticket-id="t-feed"`, `data-pending="2"`} {
		if !strings.Contains(holder, want) {
			t.Errorf("the Reviews component %q lacks %s", holder, want)
		}
	}
	button := tagAt(body, `id="task-reviews-button"`)
	if !strings.Contains(button, `aria-label="Reviews, 2 waiting"`) || strings.Contains(button, "display: none") {
		t.Errorf("the Reviews button of a task with reviews waiting is shown and says how many: %q", button)
	}
	if !strings.Contains(body, `x-text="buttonCount">2</span>`) {
		t.Error("the button paints how many wait before the browser reads them")
	}
	if inRow := body[strings.Index(body, `<div class="[ actions ]">`):]; strings.Index(inRow, `id="task-reviews-button"`) > strings.Index(inRow, `id="task-note-button"`) {
		t.Error("the Reviews button leads the title row's actions")
	}
	for _, want := range []string{
		`id="task-reviews"`,
		"Review requests page",
		`href="/projects/p1/reviews"`,
		`<script id="reviews-seed" type="application/json">`,
		`"s-server":"plain claude · build the server"`,
		`"t-feed":{"title":"Add SSE feed","href":"/projects/p1/tasks/t-feed"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task page is missing %q", want)
		}
	}
}

func TestTaskPageWithNoReviewsPaintsTheButtonHidden(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String()
	button := tagAt(body, `id="task-reviews-button"`)
	if button != "" && !strings.Contains(button, "display: none") {
		t.Errorf("a Reviews button with nothing waiting should be painted hidden: %q", button)
	}
	if strings.Contains(body, "wait on you") {
		t.Error("nothing waits, yet the page says something does")
	}
}

func TestBoardAndRailSayWhereReviewsWaitOnThePerson(t *testing.T) {
	w := board()
	w.tickets[1].PendingReviews = 2 // t-docs: no session waits, yet reviews do
	w.tickets[0].PendingReviews = 1
	body := get(t, newTestHandler(t, w), "/projects/p1").Body.String()
	card := body[strings.Index(body, `<article class="[ card ]" data-task-id="t-docs"`):]
	card = card[:strings.Index(card, "</article>")]
	// The reviews have their own badge; the card's dot is about sessions only.
	if !strings.Contains(card, `<span class="[ badge reviews ]">`) || !strings.Contains(card, "2 reviews</span>") {
		t.Errorf("the t-docs card lacks its reviews badge:\n%s", card)
	}
	if strings.Contains(card, `data-state="waiting"`) {
		t.Errorf("no session of t-docs waits, yet its card says one does:\n%s", card)
	}
	for _, want := range []string{
		`href="/projects/p1/reviews"`,
		`x-text="reviewsCount">3</span>`,
		`<template x-if="card.hasReviews">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the project page lacks %q", want)
		}
	}
}

func TestRailHidesTheReviewsBadgeWhenNothingWaits(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1").Body.String()
	if !strings.Contains(body, `x-show="hasReviews" style="display: none"><span x-text="reviewsCount">0</span>`) {
		t.Error("with nothing waiting, the Review requests badge should be painted hidden")
	}
}

func TestReviewInboxPageNamesTheTasksAndSessions(t *testing.T) {
	w := architectWorld()
	w.tickets[0].PendingReviews = 2
	rec := get(t, newTestHandler(t, w), "/projects/p1/reviews")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`<h1>Reviews</h1>`,
		`x-data="reviewsInbox" data-seed="reviews-seed"`,
		`2 reviews wait on you`,
		`"t-docs":{"title":"Write docs","href":"/projects/p1/tasks/t-docs"}`,
		`"s-server":"plain claude · build the server"`,
		`x-for="group in groups"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the inbox lacks %q", want)
		}
	}
	if strings.Contains(body, "data-ticket-id") {
		t.Error("the project's inbox is not scoped to a task")
	}
	if rec := get(t, newTestHandler(t, w), "/projects/nope/reviews"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown project's inbox: got %d, want 404", rec.Code)
	}
}
