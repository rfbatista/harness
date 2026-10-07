package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// architectServer serves a task whose architect delegated two sessions, with
// the architect channel's routes stubbed as the Web UI contract shapes them.
// A test pushes feed messages through the returned sseFeed.
func architectServer(t *testing.T, messages []domain.TaskMessage) (*httptest.Server, *sseFeed) {
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
			{ID: "t1", ProjectID: "p1", Title: "Architect highlights", Status: domain.TicketStatusInProgress, ArchitectSessionID: &arch},
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
