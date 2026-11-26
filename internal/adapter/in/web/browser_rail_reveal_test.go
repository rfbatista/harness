package web

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// Story 6: the open task is in view in the rail's list on load, also when the
// rows the island draws after it has taken over (past the server's first
// screenful) sit above it.
func TestTheOpenTaskIsInViewWhenTheIslandDrawsRowsAboveIt(t *testing.T) {
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
	deps := railWorld()
	// Sixty todo tasks, then the open one in Backlog: the server paints a first screenful, so the open
	// task is painted near the top of the list and the other rows arrive after it, above its group's row.
	tasks := []*domain.Ticket{}
	for i := range 60 {
		tasks = append(tasks, &domain.Ticket{ID: fmt.Sprintf("t-many%d", i), ProjectID: "p1", Title: fmt.Sprintf("Task number %d", i), Status: domain.TicketStatusTodo})
	}
	tasks = append(tasks, &domain.Ticket{ID: "t-open", ProjectID: "p1", Title: "The open one", Status: domain.TicketStatusBacklog})
	deps.Tasks = fakeTickets{tasks}
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	h := &railHarness{ctx: ctx, srv: srv, feed: feed, errs: errs}

	var rows int
	var inView bool
	h.run(t, "open the page",
		chromedp.EmulateViewport(1280, 700),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-open"),
		// The island has drawn every row once Todo shows all sixty.
		railPoll(`document.querySelectorAll('nav[aria-label="Tasks"] section[data-key=todo] a[data-task-id]').length === 60`),
		chromedp.Sleep(300*1e6),
		chromedp.Evaluate(`document.querySelectorAll('nav[aria-label="Tasks"] section[data-key=todo] a[data-task-id]').length`, &rows),
		chromedp.Evaluate(`(() => { const a = document.querySelector('nav[aria-label="Tasks"] a[aria-current="page"]'); const l = document.querySelector('nav[aria-label="Tasks"] .groups'); const r = a.getBoundingClientRect(); const b = l.getBoundingClientRect(); return r.top >= b.top - 1 && r.bottom <= b.bottom + 1; })()`, &inView),
	)
	if rows != 60 || !inView {
		t.Errorf("todo rows %d, the open task in view of the list: %v", rows, inView)
	}
	h.noErrors(t)
}
