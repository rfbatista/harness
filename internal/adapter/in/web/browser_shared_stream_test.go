package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// The page event stream shared across tabs (ADR 0011): however many harness
// tabs a browser profile opens, one tab holds the only /api/stream, the
// others' pages still load, and every tab gets the live changes. The server
// is plain HTTP/1.1, so a stream per tab would spend Chrome's six connections
// per host and stall the later tabs.

// serverCount counts what the browser asks of the server: the GET
// /api/stream requests open right now and ever, and the rail's refetches of
// the task list (its resync).
type serverCount struct {
	open, opened, refetched atomic.Int32
}

func (c *serverCount) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stream" {
			c.open.Add(1)
			c.opened.Add(1)
			defer c.open.Add(-1)
		}
		if r.URL.Path == "/api/list_tickets" {
			c.refetched.Add(1)
		}
		next.ServeHTTP(w, r)
	})
}

type sharedStreamHarness struct {
	*railHarness
	server *serverCount
}

// newSharedStreamHarness serves the rail's world like newRailHarness, over
// HTTP/1.1, counting the streams.
func newSharedStreamHarness(t *testing.T) *sharedStreamHarness {
	t.Helper()
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
	mux.Handle("/", NewHandler(railWorld(), assets, nil))
	server := &serverCount{}
	b := withStream(mux)
	srv := httptest.NewUnstartedServer(server.wrap(b))
	srv.Listener = &closingListener{Listener: srv.Listener, onClose: b.shutdown}
	srv.Start()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &sharedStreamHarness{railHarness: &railHarness{ctx: ctx, srv: srv, feed: feed, errs: errs}, server: server}
}

// openTabs opens n tabs of the one browser, in order, each on the task page
// with its live rail up, and returns them with the functions that close them.
func (h *sharedStreamHarness) openTabs(t *testing.T, n int) ([]context.Context, []context.CancelFunc) {
	t.Helper()
	var tabs []context.Context
	var closers []context.CancelFunc
	for len(tabs) < n {
		tab, cancel := chromedp.NewContext(h.ctx)
		t.Cleanup(cancel)
		closers = append(closers, cancel)
		// The first Run opens the tab and closes it when its context ends:
		// run it on the tab's own context, not a timeout derived from it.
		if err := chromedp.Run(tab); err != nil {
			t.Fatal(err)
		}
		tabs = append(tabs, tab)
	}
	for i, tab := range tabs {
		load, cancel := context.WithTimeout(tab, 15*time.Second)
		err := chromedp.Run(load, chromedp.EmulateViewport(1280, 900), h.openLive("/projects/p1/tasks/t-cur"))
		cancel()
		if err != nil {
			t.Fatalf("tab %d of %d did not load: %v\nJS errors: %v", i+1, n, err, h.errs.all())
		}
	}
	return tabs, closers
}

// everyTabShows waits until each tab's rail counts want tasks in Todo.
func everyTabShows(t *testing.T, tabs []context.Context, want string) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, len(tabs))
	for i, tab := range tabs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(tab, 10*time.Second)
			defer cancel()
			errs[i] = chromedp.Run(ctx, chromedp.Poll(`document.querySelector('section[data-key=todo] h3 .count > span')?.textContent === '`+want+`'`, nil,
				chromedp.WithPollingInterval(100*time.Millisecond)))
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("tab %d never showed the change: %v", i+1, err)
		}
	}
}

func todoTicket(id, title string) map[string]any {
	return map[string]any{"ticket": &domain.Ticket{ID: id, ProjectID: "p1", Title: title, Status: domain.TicketStatusTodo}}
}

func TestEightTabsShareOnePageEventStream(t *testing.T) {
	h := newSharedStreamHarness(t)
	tabs, _ := h.openTabs(t, 8)

	waitFor(t, "the project feed followed", func() bool { return h.feed.followers() >= 1 })
	if open := h.server.open.Load(); open != 1 {
		t.Errorf("%d /api/stream requests open across 8 tabs, want 1", open)
	}
	h.feed.pushRaw(t, todoTicket("t-b0", "Review the UX for the web ui"))
	everyTabShows(t, tabs, "3")
	if open := h.server.open.Load(); open != 1 {
		t.Errorf("%d /api/stream requests open after the change, want 1", open)
	}
	h.noErrors(t)
}

// Closing the stream-holding tab hands the stream to another tab: it opens a
// new stream, the remaining tabs resync, and live changes reach them again.
func TestClosingTheStreamHoldingTabHandsTheStreamOver(t *testing.T) {
	h := newSharedStreamHarness(t)
	tabs, closers := h.openTabs(t, 3)
	waitFor(t, "the project feed followed", func() bool { return h.feed.followers() >= 1 })
	if opened := h.server.opened.Load(); opened != 1 {
		t.Fatalf("%d streams opened by 3 tabs, want 1", opened)
	}

	refetched := h.server.refetched.Load()
	closers[0]() // the first tab took the lock, so it holds the stream
	waitFor(t, "a new stream", func() bool { return h.server.opened.Load() == 2 })
	waitFor(t, "the old stream closed", func() bool { return h.server.open.Load() == 1 })
	waitFor(t, "the new stream follows the project feed", func() bool { return h.feed.followers() >= 2 })

	rest := tabs[1:]
	for i, tab := range rest {
		ctx, cancel := context.WithTimeout(tab, 10*time.Second)
		err := chromedp.Run(ctx, chromedp.Poll(`document.querySelector('[x-data=streamStatus]')?.dataset.state === 'live'`, nil,
			chromedp.WithPollingInterval(100*time.Millisecond)))
		cancel()
		if err != nil {
			t.Fatalf("tab %d never came back live: %v", i+2, err)
		}
	}
	waitFor(t, "every remaining tab resynced", func() bool { return h.server.refetched.Load()-refetched >= int32(len(rest)) })
	h.feed.pushRaw(t, todoTicket("t-b0", "Review the UX for the web ui"))
	everyTabShows(t, rest, "3")
	h.noErrors(t)
}
