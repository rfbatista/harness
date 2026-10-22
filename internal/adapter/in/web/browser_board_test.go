package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// boardDeps is a project with a task in every busy column; the in-progress
// task waits on the developer.
func boardDeps() Deps {
	return Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks: fakeTickets{[]*domain.Ticket{
			{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
			{ID: "t-docs", ProjectID: "p1", Title: "Write docs", Status: domain.TicketStatusTodo},
			{ID: "t-ship", ProjectID: "p1", Title: "Ship it", Status: domain.TicketStatusDone},
		}},
		Sessions: fakeSessions{[]*domain.Session{
			{ID: "s1", ProjectID: "p1", TicketID: "t-feed", Status: domain.SessionIdle, UpdatedAt: time.Now()},
		}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{},
	}
}

// cardsIn lists the card titles of the column with that label.
func cardsIn(label string) string {
	return fmt.Sprintf(`[...document.querySelectorAll('.board .column')].filter(c => c.getAttribute('aria-label') === %q).flatMap(c => [...c.querySelectorAll('.card .title')].map(t => t.textContent))`, label)
}

func TestProjectBoardComesAliveInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler()) // no feed: the board still works from the seed
	mux.Handle("/", NewHandler(boardDeps(), assets, nil))
	srv := newBrowserServer(mux)
	defer srv.Close()

	var heads, inProgress, todo []string
	var dot, at string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.board .column > header > .label')].map(e => e.textContent)`, &heads),
		chromedp.Evaluate(cardsIn("In progress"), &inProgress),
		chromedp.Evaluate(cardsIn("Todo"), &todo),
		chromedp.Evaluate(`document.querySelector('.board .card[data-task-id="t-feed"] .status')?.dataset.state ?? ''`, &dot),
		chromedp.Click(`.board .card[data-task-id="t-docs"] .title`, chromedp.ByQuery),
		waitForPath(srv.URL+"/projects/p1/tasks/t-docs", &at),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if got := fmt.Sprint(heads); got != "[Backlog Todo In progress Review Done]" {
		t.Errorf("columns = %s", got)
	}
	if fmt.Sprint(inProgress) != "[Add SSE feed]" || fmt.Sprint(todo) != "[Write docs]" {
		t.Errorf("cards: in progress %v, todo %v", inProgress, todo)
	}
	if dot != "waiting" {
		t.Errorf("the waiting task's card has dot %q", dot)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// TestMovingACardOnTheBoard: a card's select sends a status-only update and
// the card lands in the chosen column.
func TestMovingACardOnTheBoard(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	api := &fakeTicketsAPI{tickets: map[string]map[string]string{
		"t-docs": {"id": "t-docs", "project_id": "p1", "title": "Write docs", "status": "todo"},
	}}
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", NewHandler(boardDeps(), assets, nil))
	srv := newBrowserServer(mux)
	defer srv.Close()

	var review, todo []string
	var focused string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// A keyboard user: the select has the focus when it changes.
		chromedp.Evaluate(`document.querySelector('.board .card[data-task-id="t-docs"] select').focus()`, nil),
		setField(`.board .card[data-task-id="t-docs"] select`, "review", "change"),
		chromedp.Poll(cardsIn("Review")+`.includes('Write docs')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(cardsIn("Review"), &review),
		chromedp.Evaluate(cardsIn("Todo"), &todo),
		chromedp.Evaluate(`(document.activeElement?.tagName ?? '') + ' ' + (document.activeElement?.closest('.card')?.dataset.taskId ?? '')`, &focused),
	)
	if err != nil {
		t.Fatalf("%v\nAPI:\n%s\nJS errors: %v", err, api.log(), errs.all())
	}
	if want := `/api/update_ticket {"status":"review","ticket_id":"t-docs"}`; api.log() != want {
		t.Errorf("API calls:\n%s\nwant:\n%s", api.log(), want)
	}
	if fmt.Sprint(review) != "[Write docs]" || fmt.Sprint(todo) != "[]" {
		t.Errorf("review %v, todo %v", review, todo)
	}
	if focused != "SELECT t-docs" {
		t.Errorf("after the move the focus is on %q, want the moved card's select", focused)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// TestTasksMovedElsewhereShowOnTheBoardLive: a task an agent moves from its
// session (update_task_status) or creates arrives on the board over the
// project feed, in the right column, washed and announced; a deleted one goes;
// a message with a status this client does not know is dropped.
func TestTasksMovedElsewhereShowOnTheBoardLive(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(boardDeps(), assets, nil))
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections() // the page's event stream never ends on its own
		srv.Close()
	}()

	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`document.querySelector('.stream-bar .status')?.dataset.state === 'live'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	)
	if err != nil {
		t.Fatalf("boot: %v\nJS errors: %v", err, errs.all())
	}
	ticket := func(id, title, status string) map[string]any {
		return map[string]any{"ticket": map[string]any{"id": id, "project_id": "p1", "title": title, "status": status}}
	}
	feed.pushRaw(t, ticket("t-docs", "Write docs", "review"))                                  // moved by an agent
	feed.pushRaw(t, ticket("t-new", "Triage the backlog", "backlog"))                          // created elsewhere
	feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "t-ship"}, "deleted": true}) // deleted elsewhere
	feed.pushRaw(t, ticket("t-feed", "Add SSE feed", "someday"))                               // unknown status: dropped

	var review, backlog, todo, inProgress, announced string
	var fresh bool
	err = chromedp.Run(ctx,
		chromedp.Poll(cardsIn("Review")+`.includes('Write docs') && `+cardsIn("Backlog")+`.includes('Triage the backlog') && !`+cardsIn("Done")+`.includes('Ship it')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`JSON.stringify(`+cardsIn("Review")+`)`, &review),
		chromedp.Evaluate(`JSON.stringify(`+cardsIn("Backlog")+`)`, &backlog),
		chromedp.Evaluate(`JSON.stringify(`+cardsIn("Todo")+`)`, &todo),
		chromedp.Evaluate(`JSON.stringify(`+cardsIn("In progress")+`)`, &inProgress),
		chromedp.Evaluate(`document.querySelector('.board .card[data-task-id="t-new"]')?.hasAttribute('data-fresh') ?? false`, &fresh),
		chromedp.Evaluate(`document.querySelector('main [role=status]').textContent`, &announced),
	)
	if err != nil {
		t.Fatalf("live: %v\nJS errors: %v", err, errs.all())
	}
	if review != `["Write docs"]` || backlog != `["Triage the backlog"]` || todo != `[]` {
		t.Errorf("review %s, backlog %s, todo %s", review, backlog, todo)
	}
	if inProgress != `["Add SSE feed"]` {
		t.Errorf("a message with an unknown status must be dropped; in progress = %s", inProgress)
	}
	if !fresh {
		t.Error("the task created elsewhere is not washed")
	}
	if announced != "Ship it was removed" {
		t.Errorf("announcement = %q", announced)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// TestBrainstormAFeatureFromTheBoard: the dialog starts a product-mode
// session with no task and lands on it on the project's sessions page.
func TestBrainstormAFeatureFromTheBoard(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	deps := boardDeps()
	deps.Agents = fakeAgents{[]*domain.Agent{{ID: "pm", Name: "Product manager"}}}
	deps.Repositories = fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}}
	started := make(chan map[string]any, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/start_interactive_session", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		started <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"session":{"id":"brain-1","project_id":"p1","repository_id":"r1","agent_id":"pm","mode":"product","task":"","status":"starting","pending_approvals":0,"interactive":true,"runs_on":"server","updated_at":"2026-10-21T12:00:00Z"},"agent":{}}`)
	})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	defer srv.Close()

	var at string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(`#brainstorm-open`, chromedp.ByQuery),
		chromedp.Poll(`document.getElementById('brainstorm')?.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.SetValue(`#brainstorm-agent`, "pm", chromedp.ByQuery),
		chromedp.Click(`#brainstorm button[type=submit]`, chromedp.ByQuery),
		waitForPath(srv.URL+"/projects/p1/sessions?session=brain-1", &at),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	select {
	case body := <-started:
		if _, has := body["ticket_id"]; has || body["mode"] != "product" || body["repository_id"] != "r1" || body["agent_id"] != "pm" || body["runs_on"] != "server" {
			t.Errorf("start body = %v", body)
		}
	default:
		t.Error("no start request reached the API")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// TestFeatureCuesFollowTheFeedOnTheBoard: a task that becomes a child over
// the feed names its parent on its card, and the parent's card counts it,
// without a reload.
func TestFeatureCuesFollowTheFeedOnTheBoard(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	deps := boardDeps()
	deps.Tasks = fakeTickets{[]*domain.Ticket{
		{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
		{ID: "t-docs", ProjectID: "p1", Title: "Write docs", Status: domain.TicketStatusTodo, ParentID: "t-feed"},
	}}
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	defer srv.Close()

	cue := func(id string) string {
		return fmt.Sprintf(`(c => (c?.querySelector('[data-parent-title]')?.textContent ?? '-') + '|' + (c?.querySelector('[data-children]')?.textContent ?? '-'))(document.querySelector('.board .card[data-task-id=%q]'))`, id)
	}
	var painted, child, parent, arrived string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(cue("t-docs")+" + ' / ' + "+cue("t-feed"), &painted),
		chromedp.Poll(`document.querySelector('[data-stream-status]')?.dataset.state === 'live' || true`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.ActionFunc(func(context.Context) error {
			for feed.followers() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			// A new child of Add SSE feed, and Write docs moved under another parent that is not listed.
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "t-new", "project_id": "p1", "title": "Add the SSE docs", "status": "todo", "parent_id": "t-feed"}})
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "t-docs", "project_id": "p1", "title": "Write docs", "status": "todo", "parent_id": ""}})
			return nil
		}),
		chromedp.Poll(`document.querySelector('.board .card[data-task-id="t-new"] [data-parent-title]') !== null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(cue("t-new"), &arrived),
		chromedp.Evaluate(cue("t-docs"), &child),
		chromedp.Evaluate(cue("t-feed"), &parent),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if painted != "Add SSE feed|- / -|1 task" {
		t.Errorf("first paint cues = %q", painted)
	}
	if arrived != "Add SSE feed|-" || child != "-|-" || parent != "-|1 task" {
		t.Errorf("after the feed: new %q, docs %q, feed %q", arrived, child, parent)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// TestAChildArrivingOverTheFeedJoinsTheFeatureSection: on the feature
// task's page, a child an agent creates shows in the section as it is
// created, and a status move follows.
func TestAChildArrivingOverTheFeedJoinsTheFeatureSection(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	deps := boardDeps()
	deps.Tasks = fakeTickets{[]*domain.Ticket{
		{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
		{ID: "t-docs", ProjectID: "p1", Title: "Write docs", Status: domain.TicketStatusTodo, ParentID: "t-feed"},
	}}
	deps.Repositories = fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}}
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	defer srv.Close()

	rows := `[...document.querySelectorAll('.feature .row[data-child-id]')].map(r => r.dataset.childId + ':' + r.querySelector('.badge').textContent + ':' + r.querySelector('.meta').textContent.trim())`
	var painted, after []string
	var count string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(`document.querySelector('.feature [data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(rows, &painted),
		chromedp.ActionFunc(func(context.Context) error {
			for feed.followers() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "t-new", "project_id": "p1", "title": "Write the feed tests", "status": "backlog", "parent_id": "t-feed"}})
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "t-docs", "project_id": "p1", "title": "Write docs", "status": "in_progress", "parent_id": "t-feed"}})
			return nil
		}),
		chromedp.Poll(`document.querySelector('.feature .row[data-child-id="t-new"]') !== null && document.querySelector('.feature .row[data-child-id="t-docs"] .badge')?.textContent === 'In progress'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(rows, &after),
		chromedp.Evaluate(`document.querySelector('.feature .section-head .badge').textContent`, &count),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if fmt.Sprint(painted) != "[t-docs:Todo:No documents yet]" {
		t.Errorf("first paint rows = %v", painted)
	}
	if fmt.Sprint(after) != "[t-docs:In progress:No documents yet t-new:Backlog:No documents yet]" || count != "2" {
		t.Errorf("after the feed: rows %v, count %q", after, count)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
