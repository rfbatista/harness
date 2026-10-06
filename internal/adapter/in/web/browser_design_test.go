package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// artifactsAPI stands in for the server's artifact routes, in the shape of
// Contract: Server ↔ Web UI — Artifacts: the list, the view route (with the
// contract's headers), and the session's event stream.
type artifactsAPI struct {
	list []map[string]any
	feed *sseFeed
}

func artifactDTO(id, kind, title, note, path string, revision int, at time.Time) map[string]any {
	return map[string]any{
		"id": id, "session_id": "s1", "ticket_id": "t1", "project_id": "p1", "kind": kind,
		"title": title, "note": note, "path": path, "url": nil, "mime": "text/html", "size_bytes": 64,
		"revision": revision, "created_at": at.Format(time.RFC3339), "updated_at": at.Format(time.RFC3339),
	}
}

func (a *artifactsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/artifacts":
		if r.URL.Query().Get("session_id") == "" {
			http.Error(w, `{"error":"session_id required","code":"INVALID_INPUT"}`, 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": a.list})
	case strings.HasSuffix(r.URL.Path, "/view/"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'self' data: blob:; frame-ancestors 'self'")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", `"`+r.URL.Query().Get("rev")+`"`)
		fmt.Fprintf(w, "<!doctype html><title>artifact</title><p>rev %s</p>", r.URL.Query().Get("rev"))
	case r.URL.Path == "/api/sessions/s1/events":
		a.feed.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

// TestDesignTabShowsArtifactsLive: the Design tab lists what the session
// published, renders the selected page in a sandboxed frame, counts a publish
// that arrives behind the Agent tab, and refreshes the chosen card in place
// when it is re-published. Titles are text, never markup.
func TestDesignTabShowsArtifactsLive(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	now := time.Now()
	hostile := `<img src=x onerror="document.title='pwned'">`
	api := &artifactsAPI{
		list: []map[string]any{artifactDTO("a1", "page", hostile, "first cut", "design/hero.html", 1, now.Add(-time.Minute))},
		feed: &sseFeed{},
	}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Pricing page", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions: fakeSessions{[]*domain.Session{{
			ID: "s1", ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: "design the pricing page", Mode: domain.SessionMode("design"),
			Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: now,
		}}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/events", http.NotFoundHandler())               // the project feed is not under test
	mux.Handle("/api/sessions/s1/terminal", http.NotFoundHandler()) // nor the terminal
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	designTab := `[...document.querySelectorAll('[role=tab]')].find(b => b.textContent.trim().startsWith('Design'))`
	card := `document.querySelector('[aria-label="Design"] .artifact-card')`
	var cardTitle, cardKind, agentLine, sandbox, frameSrc string
	var imgInCard, badgeBefore, panelVisible bool
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('main section .cluster span:nth-child(2)')?.textContent ?? ''`, &agentLine),
		chromedp.Evaluate(designTab+`.querySelector('.badge') !== null`, &badgeBefore),
		clickButton(`[role=tablist]`, "Design"),
		chromedp.Poll(`!!`+card, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(card+`.querySelector('.title').textContent`, &cardTitle),
		chromedp.Evaluate(card+`.querySelector('img') !== null`, &imgInCard),
		chromedp.Evaluate(card+`.querySelector('.badge').textContent`, &cardKind),
		chromedp.Poll(`!!document.querySelector('[aria-label="Design"] iframe')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[aria-label="Design"]')).display !== 'none'`, &panelVisible),
		chromedp.Evaluate(`document.querySelector('[aria-label="Design"] iframe').getAttribute('sandbox')`, &sandbox),
		chromedp.Evaluate(`document.querySelector('[aria-label="Design"] iframe').getAttribute('src')`, &frameSrc),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if agentLine != "plain claude as design" {
		t.Errorf("detail agent line = %q, want the mode as a word", agentLine)
	}
	if badgeBefore {
		t.Error("the Design tab carries a badge before anything arrived")
	}
	if cardTitle != hostile || imgInCard {
		t.Errorf("title rendered as markup: text=%q img=%v", cardTitle, imgInCard)
	}
	if cardKind != "page" {
		t.Errorf("kind badge = %q", cardKind)
	}
	if !panelVisible {
		t.Error("the Design panel stays hidden after its tab is selected")
	}
	if sandbox != "allow-scripts" {
		t.Errorf("iframe sandbox = %q, want allow-scripts only", sandbox)
	}
	if frameSrc != "/api/artifacts/a1/view/?rev=1" {
		t.Errorf("iframe src = %q", frameSrc)
	}

	// Behind the Agent tab, a publish counts on the Design tab and is announced.
	// The stream replays its history on connect: the artifact already listed
	// arrives again first and must count for nothing.
	waitFor(t, "the session stream", func() bool { return api.feed.followers() >= 1 })
	if err := chromedp.Run(ctx, clickButton(`[role=tablist]`, "Agent")); err != nil {
		t.Fatal(err)
	}
	api.feed.pushRaw(t, map[string]any{
		"seq": 6, "session_id": "s1", "type": "artifact", "text": "first cut",
		"artifact": api.list[0], "at": now.Add(-time.Minute).Format(time.RFC3339),
	})
	api.feed.pushRaw(t, map[string]any{
		"seq": 7, "session_id": "s1", "type": "artifact", "text": "the card",
		"artifact": artifactDTO("a2", "image", "Card", "the card", "design/card.png", 1, now), "at": now.Format(time.RFC3339),
	})
	var badge, announced string
	err = chromedp.Run(ctx,
		chromedp.Poll(designTab+`.querySelector('.badge')?.textContent === '1'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(designTab+`.querySelector('.badge').textContent`, &badge),
		chromedp.Evaluate(`document.querySelector('[role=status][aria-live=polite]').textContent`, &announced),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if badge != "1" || announced != "New artifact: Card" {
		t.Errorf("badge = %q, announcement = %q", badge, announced)
	}

	// Open the Design tab (the badge clears), pick the page by hand, then
	// re-publish it: the chosen card re-renders in place (one card, rev 2,
	// highlighted) and the frame reloads with the new revision.
	pageCard := `[...document.querySelectorAll('[aria-label="Design"] .artifact-card')].find(c => c.querySelector('.badge').textContent === 'page')`
	err = chromedp.Run(ctx,
		clickButton(`[role=tablist]`, "Design"),
		chromedp.Poll(`!!`+pageCard, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(pageCard+`.click(); true`, nil),
		chromedp.Poll(pageCard+`.getAttribute('aria-selected') === 'true'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	api.feed.pushRaw(t, map[string]any{
		"seq": 8, "session_id": "s1", "type": "artifact", "text": "tighter spacing",
		"artifact": artifactDTO("a1", "page", "Hero", "tighter spacing", "design/hero.html", 2, now.Add(time.Second)), "at": now.Format(time.RFC3339),
	})
	var cards []string
	var badgeAfter, fresh bool
	err = chromedp.Run(ctx,
		chromedp.Poll(`[...document.querySelectorAll('[aria-label="Design"] .artifact-card .meta span:first-child')].some(s => s.textContent === 'rev 2')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label="Design"] .artifact-card .title')].map(e => e.textContent)`, &cards),
		chromedp.Evaluate(`document.querySelector('[aria-label="Design"] .artifact-card').hasAttribute('data-fresh')`, &fresh),
		chromedp.Evaluate(designTab+`.querySelector('.badge') !== null`, &badgeAfter),
		chromedp.Evaluate(`document.querySelector('[aria-label="Design"] iframe')?.getAttribute('src') ?? ''`, &frameSrc),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if got := fmt.Sprint(cards); got != "[Hero Card]" {
		t.Errorf("cards = %s, want the re-published page first, once", got)
	}
	if !fresh {
		t.Error("the re-published card is not highlighted")
	}
	if badgeAfter {
		t.Error("the badge did not clear when the tab opened")
	}
	if frameSrc != "/api/artifacts/a1/view/?rev=2" {
		t.Errorf("iframe after re-publish = %q", frameSrc)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors on the page:\n%s", strings.Join(e, "\n"))
	}
}
