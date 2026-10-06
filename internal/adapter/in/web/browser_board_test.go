package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	srv := httptest.NewServer(mux)
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
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var review, todo []string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1"),
		chromedp.Poll(`document.querySelector('[data-ssr]') === null`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		setField(`.board .card[data-task-id="t-docs"] select`, "review", "change"),
		chromedp.Poll(cardsIn("Review")+`.includes('Write docs')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(cardsIn("Review"), &review),
		chromedp.Evaluate(cardsIn("Todo"), &todo),
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
	srv := httptest.NewServer(mux)
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
