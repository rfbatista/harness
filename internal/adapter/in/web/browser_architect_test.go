package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// architectServer serves a task whose architect delegated two sessions, with
// the architect channel's routes stubbed as the Web UI contract shapes them.
// A test pushes feed messages through the returned sseFeed.
func architectServer(t *testing.T, messages []domain.TaskMessage, reviews ...*domain.ReviewRequest) (*httptest.Server, *sseFeed) {
	t.Helper()
	if messages == nil {
		messages = []domain.TaskMessage{} // the server sends [], never null
	}
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	now := time.Now()
	arch := "s-arch"
	next := now.Add(6*time.Minute + 30*time.Second)
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks: fakeTickets{[]*domain.Ticket{
			{ID: "t1", ProjectID: "p1", Title: "Architect highlights", Status: domain.TicketStatusInProgress, ArchitectSessionID: &arch, PendingReviews: pendingOf(reviews)},
		}},
		Agents:       fakeAgents{[]*domain.Agent{{ID: "a-arch", Name: "software-architect"}, {ID: "a-go", Name: "go-developer"}, {ID: "a-web", Name: "frontend-developer"}}},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions: fakeSessions{[]*domain.Session{
			{ID: "s-arch", ProjectID: "p1", TicketID: "t1", AgentID: "a-arch", Mode: domain.SessionModeArchitect, Role: domain.RoleArchitect, ArchitectSessionID: &arch,
				Task: "Shape the architect highlights", Status: domain.SessionRunning, UpdatedAt: now.Add(-time.Hour)},
			{ID: "s-server", ProjectID: "p1", TicketID: "t1", AgentID: "a-go", ParentSessionID: "s-arch", Role: domain.RoleDelegate, ArchitectSessionID: &arch,
				Task: "Server: architect channel", Status: domain.SessionRunning, UpdatedAt: now.Add(-30 * time.Minute),
				StatusCheck: &domain.StatusCheck{TaskID: "t1", ArchitectSessionID: arch, DelegateSessionID: "s-server", EveryMinutes: 10, NextAt: &next, State: domain.StatusCheckActive}},
			{ID: "s-web", ProjectID: "p1", TicketID: "t1", AgentID: "a-web", ParentSessionID: "s-arch", Role: domain.RoleDelegate, ArchitectSessionID: &arch,
				Task: "Web UI: review inbox", Status: domain.SessionIdle, UpdatedAt: now.Add(-20 * time.Minute)},
			{ID: "s-peer", ProjectID: "p1", TicketID: "t1", Task: "A person's own session", Status: domain.SessionRunning, UpdatedAt: now},
		}},
	}, assets, nil)

	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.HandleFunc("GET /api/task_messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"messages": messages})
	})
	mux.HandleFunc("POST /api/set_status_check", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DelegateSessionID string `json:"delegate_session_id"`
			EveryMinutes      int    `json:"every_minutes"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		check := domain.StatusCheck{TaskID: "t1", ArchitectSessionID: arch, DelegateSessionID: body.DelegateSessionID, EveryMinutes: body.EveryMinutes, State: domain.StatusCheckActive}
		if body.EveryMinutes == 0 {
			check.State = domain.StatusCheckPaused
		} else {
			at := time.Now().Add(time.Duration(body.EveryMinutes)*time.Minute + 30*time.Second)
			check.NextAt = &at
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status_check": check})
	})
	var mu sync.Mutex
	mux.HandleFunc("GET /api/review_requests", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		list := []*domain.ReviewRequest{}
		for _, rv := range reviews {
			if r.URL.Query().Get("state") == "" || string(rv.State) == r.URL.Query().Get("state") {
				list = append(list, rv)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"review_requests": list})
	})
	mux.HandleFunc("POST /api/respond_review_request", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ReviewID string `json:"review_id"`
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		for _, rv := range reviews {
			if rv.ID == body.ReviewID {
				now := time.Now()
				rv.State, rv.ResponseNote, rv.RespondedAt, rv.UpdatedAt = domain.ReviewState(body.Decision), body.Note, &now, now
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"review_request": rv, "delivered": false})
				return
			}
		}
		http.Error(w, `{"error":"no such review","code":"REVIEW_NOT_FOUND"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /api/artifacts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"artifacts":[]}`))
	})
	mux.Handle("/api/", http.NotFoundHandler()) // terminals are not under test
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the page's event streams never end on their own
		srv.Close()
	})
	return srv, feed
}

func pendingOf(reviews []*domain.ReviewRequest) int {
	n := 0
	for _, r := range reviews {
		if r.State == domain.ReviewPending {
			n++
		}
	}
	return n
}

// noBanner fails the test when the page shows an error banner.
func noBanner(t *testing.T, ctx context.Context) {
	t.Helper()
	var banners []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('.banner[role=alert]')].map(b => b.textContent.replace(/\s+/g, ' ').trim())`, &banners)); err != nil {
		t.Fatal(err)
	}
	if len(banners) > 0 {
		t.Errorf("the page shows an error: %q", banners)
	}
}

// shot saves a screenshot for a person to look at when ARCHITECT_SHOTS names
// a directory; otherwise it does nothing.
func shot(name string) chromedp.Action {
	dir := os.Getenv("ARCHITECT_SHOTS")
	if dir == "" {
		return chromedp.ActionFunc(func(ctxt context.Context) error { return nil })
	}
	var buf []byte
	return chromedp.Tasks{
		chromedp.FullScreenshot(&buf, 90),
		chromedp.ActionFunc(func(ctxt context.Context) error { return os.WriteFile(filepath.Join(dir, name+".png"), buf, 0o644) }),
	}
}

// TestArchitectLeadsTheTaskAndTalksWithItsDelegates: the architect's group
// leads the list, a delegate's row says its last report and next check, and
// the Conversation tab shows the messages and takes new ones live.
func TestArchitectLeadsTheTaskAndTalksWithItsDelegates(t *testing.T) {
	report := domain.TaskMessage{
		ID: "m1", TaskID: "t1", FromSessionID: "s-server", ToSessionID: "s-arch", Kind: domain.MessageStatusReport,
		Status: domain.ReportReadyForReview, Body: "Port and scheduler done.\nTests green.", DocumentIDs: []string{}, ArtifactIDs: []string{},
		Delivered: true, CreatedAt: time.Now().Add(-4 * time.Minute),
	}
	srv, feed := architectServer(t, []domain.TaskMessage{report})
	ctx, errs := browser(t)

	rows := `[...document.querySelectorAll('[role=listbox] .row')].map(r => r.querySelector('.title').textContent.trim() + ' | ' + r.querySelector('.meta').textContent.trim())`
	var order []string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`[...document.querySelectorAll('[role=listbox] .row[data-role=delegate] .meta')].some(m => m.textContent.includes('ready for review'))`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(rows, &order),
		shot("architect-list"),
	); err != nil {
		var dump []string
		_ = chromedp.Run(ctx, chromedp.Evaluate(rows, &dump))
		t.Fatalf("%v\nrows: %q\nJS errors: %v", err, dump, errs.all())
	}
	want := []string{
		"architect Shape the architect highlights | software-architect as architect · 1h",
		"Web UI: review inbox | frontend-developer · no report yet",
		"Server: architect channel | go-developer · ready for review 4m · check in 6m",
		"A person's own session | plain claude · now",
	}
	if len(order) != len(want) {
		t.Fatalf("rows = %q, want %q", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, order[i], want[i])
		}
	}

	// The architect's Conversation: every message; then one arrives live.
	var bodies []string
	var badge string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('[role=listbox] .row')][0].click()`, nil),
		chromedp.Poll(`[...document.querySelectorAll('[role=tab]')].some(b => b.textContent.trim() === 'Conversation')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[role=tab]')].find(b => b.textContent.trim() === 'Conversation').click()`, nil),
		chromedp.Poll(`document.querySelectorAll('[aria-label=Conversation] .message').length === 1`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	reply := report
	reply.ID, reply.FromSessionID, reply.ToSessionID, reply.Kind, reply.Status = "m2", "s-arch", "s-server", domain.MessageReply, ""
	reply.Verdict, reply.InReplyTo, reply.Body, reply.Delivered, reply.CreatedAt = domain.VerdictChangesRequested, "m1", "Rename SetStatusCheck's argument.", false, time.Now()
	feed.pushRaw(t, map[string]any{"task_message": reply})
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.querySelectorAll('[aria-label=Conversation] .message').length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label=Conversation] .message')].map(m => m.textContent.replace(/\s+/g, ' ').trim())`, &bodies),
		chromedp.Evaluate(`(() => { const t = [...document.querySelectorAll('[role=tab]')].find(b => b.textContent.includes('Conversation')); return t.querySelector('.badge')?.textContent ?? ''; })()`, &badge),
		shot("architect-conversation"),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for _, want := range []string{"the architect → go-developer", "reply", "changes requested", "Reply to “Port and scheduler done.”", "Not delivered yet"} {
		if !strings.Contains(bodies[1], want) {
			t.Errorf("the reply reads %q, missing %q", bodies[1], want)
		}
	}
	if badge != "" {
		t.Errorf("the tab is in front, yet its badge says %q", badge)
	}
	noBanner(t, ctx)
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestPersonPausesAndResumesADelegatesStatusChecks: the selected delegate's
// header shows its loop; Pause and Resume change it, and its row follows.
func TestPersonPausesAndResumesADelegatesStatusChecks(t *testing.T) {
	srv, _ := architectServer(t, nil)
	ctx, errs := browser(t)

	bar := `(document.querySelector('[aria-label="Status checks"]')?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	serverRow := `[...document.querySelectorAll('[role=listbox] .row')].find(r => r.textContent.includes('Server: architect channel'))`
	var before, paused, resumed, row string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('[role=listbox] .row[data-role=delegate]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(serverRow+`.click()`, nil),
		chromedp.Poll(`!!document.querySelector('[aria-label="Status checks"] button')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(bar, &before),
		shot("status-check-active"),
		chromedp.Click(`[aria-label="Status checks"] button`, chromedp.ByQuery),
		chromedp.Poll(`(`+bar+`).includes('checks paused')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(bar, &paused),
		chromedp.Evaluate(serverRow+`.querySelector('.meta').textContent`, &row),
		chromedp.Click(`[aria-label="Status checks"] button`, chromedp.ByQuery),
		chromedp.Poll(`(`+bar+`).includes('checks every 10m')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(bar, &resumed),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for _, c := range []struct{ got, want string }{
		{before, "checks every 10m · next in 6m · no check yet"},
		{before, "Pause"},
		{paused, "Resume"},
		{row, "checks paused"},
		{resumed, "Pause"},
	} {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%q lacks %q", c.got, c.want)
		}
	}
	noBanner(t, ctx)
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestPersonAnswersTheArchitectsReviewRequests: the band shows what waits on
// the person; requesting changes needs a note; an answer settles the request
// under Earlier reviews and says when the architect will get it.
func TestPersonAnswersTheArchitectsReviewRequests(t *testing.T) {
	now := time.Now()
	review := func(id, subject string, minutes int) *domain.ReviewRequest {
		at := now.Add(-time.Duration(minutes) * time.Minute)
		return &domain.ReviewRequest{ID: id, TaskID: "t1", ProjectID: "p1", ArchitectSessionID: "s-arch", AboutSessionID: "s-server",
			Subject: subject, Body: "Three specs and two contracts.\nPlease check the status authority rule.", DocumentIDs: []string{}, ArtifactIDs: []string{},
			State: domain.ReviewPending, CreatedAt: at, UpdatedAt: at}
	}
	srv, feed := architectServer(t, nil, review("rv1", "Spec set ready for sign-off", 12), review("rv2", "Server API shape", 3))
	ctx, errs := browser(t)

	band := `(document.querySelector('[aria-label="Review requests"]')?.textContent ?? '').replace(/\s+/g, ' ').trim()`
	cards := `[...document.querySelectorAll('[aria-label="Review requests"] .review')]`
	var first, problem, after string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(cards+`.length === 2`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(band, &first),
		shot("reviews-band"),
		// Request changes without a note: stopped at the field.
		chromedp.Evaluate(cards+`.find(c => c.textContent.includes('Spec set')).querySelectorAll('button')[1].click()`, nil),
		chromedp.Poll(`!!document.querySelector('[aria-label="Review requests"] textarea[aria-invalid=true]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[aria-label="Review requests"] textarea[aria-invalid=true]').closest('.field').querySelector('.error').textContent`, &problem),
		// With a note it is sent.
		chromedp.SendKeys(`[aria-label="Review requests"] textarea[aria-invalid=true]`, "Split the contract in two.", chromedp.ByQuery),
		chromedp.Evaluate(cards+`.find(c => c.textContent.includes('Spec set')).querySelectorAll('button')[1].click()`, nil),
		chromedp.Poll(cards+`.filter(c => !c.closest('details')).length === 1`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[aria-label="Review requests"] details summary').click()`, nil),
		chromedp.Evaluate(band, &after),
		shot("reviews-answered"),
	); err != nil {
		var dump, html string
		_ = chromedp.Run(ctx, chromedp.Evaluate(band, &dump), chromedp.Evaluate(cards+`[0]?.outerHTML ?? ''`, &html))
		t.Fatalf("%v\nband: %s\nfirst card: %s\nJS errors: %v", err, dump, html, errs.all())
	}
	for _, c := range []struct{ got, want string }{
		{first, "2 reviews wait on you"},
		{first, "from the architect · about go-developer · Server: architect channel"},
		{problem, "Say what should change"},
		{after, "1 review waits on you"},
		{after, "Earlier reviews (1)"},
		{after, "changes requested"},
		{after, "Your note: Split the contract in two."},
		{after, "Saved. The architect gets your answer when its current turn ends."},
	} {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%q lacks %q", c.got, c.want)
		}
	}

	// The architect raises another over the feed: it shows and is announced.
	feed.pushRaw(t, map[string]any{"review_request": review("rv3", "Board badge wording", 0)})
	var announced string
	if err := chromedp.Run(ctx,
		chromedp.Poll(cards+`.filter(c => !c.closest('details')).length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=status][aria-live=polite]').textContent`, &announced),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if announced != "The architect asks for your review: Board badge wording" {
		t.Errorf("announced %q", announced)
	}
	noBanner(t, ctx)
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestReviewInboxAndBoardFollowWhatWaitsOnThePerson: the project's inbox
// lists the pending requests by task and answers them; the board's badge and
// the rail's Reviews count follow the ticket change the server sends after.
func TestReviewInboxAndBoardFollowWhatWaitsOnThePerson(t *testing.T) {
	now := time.Now()
	review := func(id, subject string) *domain.ReviewRequest {
		return &domain.ReviewRequest{ID: id, TaskID: "t1", ProjectID: "p1", ArchitectSessionID: "s-arch", Subject: subject, Body: "Look.",
			DocumentIDs: []string{}, ArtifactIDs: []string{}, State: domain.ReviewPending, CreatedAt: now, UpdatedAt: now}
	}
	srv, feed := architectServer(t, nil, review("rv1", "Spec set ready for sign-off"), review("rv2", "Server API shape"))
	ctx, errs := browser(t)

	cards := `[...document.querySelectorAll('main .review')]`
	var group, headline string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/reviews"),
		chromedp.Poll(cards+`.length === 2`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('main .reviews h2').textContent.trim()`, &group),
		shot("reviews-inbox"),
		chromedp.Evaluate(cards+`[0].querySelector('button').click()`, nil),
		chromedp.Poll(cards+`.length === 1`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('main .toolbar [x-text=headline]').textContent`, &headline),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if group != "Architect highlights" || headline != "1 review waits on you" {
		t.Errorf("group %q, headline %q", group, headline)
	}

	// The board: the card's badge and the rail's count follow the ticket change.
	badge := `(document.querySelector('.board .card[data-task-id=t1] .badge[data-tone=attention]')?.textContent ?? '')`
	railCount := `(document.querySelector('nav [x-text=reviewsCount]')?.textContent ?? '')`
	var before, after, railAfter string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Poll(`(`+badge+`) !== ''`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(badge, &before),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	waitFor(t, "the board's feed", func() bool { return feed.followers() >= 1 })
	arch := "s-arch"
	feed.pushRaw(t, map[string]any{"ticket": &domain.Ticket{ID: "t1", ProjectID: "p1", Title: "Architect highlights", Status: domain.TicketStatusInProgress, ArchitectSessionID: &arch, PendingReviews: 1}})
	if err := chromedp.Run(ctx,
		chromedp.Poll(`(`+badge+`) === '1 review'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(badge, &after),
		chromedp.Evaluate(railCount, &railAfter),
		shot("board-reviews"),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if before != "2 reviews" || after != "1 review" || railAfter != "1" {
		t.Errorf("badge %q → %q, rail %q", before, after, railAfter)
	}
	noBanner(t, ctx)
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
