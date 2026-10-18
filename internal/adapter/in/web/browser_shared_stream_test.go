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

// streamCount counts the GET /api/stream requests open right now, and ever.
type streamCount struct {
	open, opened atomic.Int32
}

func (c *streamCount) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stream" {
			c.open.Add(1)
			c.opened.Add(1)
			defer c.open.Add(-1)
		}
		next.ServeHTTP(w, r)
	})
}

type sharedStreamHarness struct {
	*railHarness
	streams *streamCount
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
	streams := &streamCount{}
	b := withStream(mux)
	srv := httptest.NewUnstartedServer(streams.wrap(b))
	srv.Listener = &closingListener{Listener: srv.Listener, onClose: b.shutdown}
	srv.Start()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return &sharedStreamHarness{railHarness: &railHarness{ctx: ctx, srv: srv, feed: feed, errs: errs}, streams: streams}
}

// openTabs opens n tabs of the one browser, each on the task page with its
// live rail up. The first tab is the browser's own first target.
func (h *sharedStreamHarness) openTabs(t *testing.T, n int) []context.Context {
	t.Helper()
	tabs := []context.Context{h.ctx}
	for len(tabs) < n {
		tab, cancel := chromedp.NewContext(h.ctx)
		t.Cleanup(cancel)
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
	return tabs
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
	tabs := h.openTabs(t, 8)

	waitFor(t, "the project feed followed", func() bool { return h.feed.followers() >= 1 })
	if open := h.streams.open.Load(); open != 1 {
		t.Errorf("%d /api/stream requests open across 8 tabs, want 1", open)
	}
	h.feed.pushRaw(t, todoTicket("t-b0", "Review the UX for the web ui"))
	everyTabShows(t, tabs, "3")
	if open := h.streams.open.Load(); open != 1 {
		t.Errorf("%d /api/stream requests open after the change, want 1", open)
	}
	h.noErrors(t)
}
