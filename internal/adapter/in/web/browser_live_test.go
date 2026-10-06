package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sseFeed stands in for /api/events: every connected follower (the task page
// and the rail each hold one) gets what the test pushes.
type sseFeed struct {
	mu    sync.Mutex
	conns []chan []byte
}

func (f *sseFeed) followers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.conns)
}

// pushRaw sends any JSON value to every follower, as one data: line.
func (f *sseFeed) pushRaw(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c <- b
	}
}

func (f *sseFeed) push(t *testing.T, change ports.SessionChange) { f.pushRaw(t, change) }

func (f *sseFeed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)
	flusher.Flush()
	c := make(chan []byte, 16)
	f.mu.Lock()
	f.conns = append(f.conns, c)
	f.mu.Unlock()
	for {
		select {
		case b := <-c:
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// TestSessionsStartedElsewhereShowUpLive: a session an agent starts on the
// open task (start_task_session) appears on the task page as it starts,
// highlighted and announced, naming who started it; the rail's counts follow,
// for the open task and for another.
func TestSessionsStartedElsewhereShowUpLive(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	now := time.Now()
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks: fakeTickets{[]*domain.Ticket{
			{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
			{ID: "t2", ProjectID: "p1", Title: "Write docs", Status: domain.TicketStatusTodo},
		}},
		Agents:       fakeAgents{[]*domain.Agent{{ID: "a-lead", Name: "Lead dev"}}},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions: fakeSessions{[]*domain.Session{{
			ID: "lead", ProjectID: "p1", TicketID: "t1", AgentID: "a-lead", Task: "implement the feed",
			Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: now,
		}}},
	}, assets, nil)
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", http.NotFoundHandler()) // terminals are not under test
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer func() {
		srv.CloseClientConnections() // the page's event streams never end on their own
		srv.Close()
	}()

	count := func(task string) string {
		return fmt.Sprintf(`(() => { const c = document.querySelector('nav [data-task-id=%q] .count'); return c && c.style.display !== 'none' ? c.textContent : ''; })()`, task)
	}
	var t1Before, t2Before string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('[role=listbox] .row')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(count("t1"), &t1Before),
		chromedp.Evaluate(count("t2"), &t2Before),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if t1Before != "1" || t2Before != "" {
		t.Fatalf("rail before: t1=%q t2=%q", t1Before, t2Before)
	}
	waitFor(t, "both followers", func() bool { return feed.followers() >= 2 })

	feed.push(t, ports.SessionChange{Session: &domain.Session{
		ID: "peer", ProjectID: "p1", TicketID: "t1", Task: "Write the feed tests", Status: domain.SessionRunning,
		Interactive: true, RunsOn: domain.RunnerServer, ParentSessionID: "lead", UpdatedAt: now,
	}})
	feed.push(t, ports.SessionChange{Session: &domain.Session{
		ID: "docs", ProjectID: "p1", TicketID: "t2", Task: "Draft the docs", Status: domain.SessionIdle, UpdatedAt: now,
	}})

	peerRow := `[...document.querySelectorAll('[role=listbox] .row')].find(r => r.textContent.includes('Write the feed tests'))`
	var meta, announced, t1After, t2After, t2Dot string
	var fresh bool
	err = chromedp.Run(ctx,
		chromedp.Poll(`!!`+peerRow, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(peerRow+`.querySelector('.meta').textContent`, &meta),
		chromedp.Evaluate(peerRow+`.hasAttribute('data-fresh')`, &fresh),
		chromedp.Evaluate(`document.querySelector('[role=status][aria-live=polite]').textContent`, &announced),
		chromedp.Poll(count("t2")+` === '1'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(count("t1"), &t1After),
		chromedp.Evaluate(count("t2"), &t2After),
		chromedp.Evaluate(`document.querySelector('nav [data-task-id=t2] .status').dataset.state`, &t2Dot),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if want := "plain claude · started by Lead dev · "; len(meta) < len(want) || meta[:len(want)] != want {
		t.Errorf("row meta = %q, want it to start %q", meta, want)
	}
	if !fresh {
		t.Error("the arriving row is not highlighted")
	}
	if announced != "Lead dev started a session: Write the feed tests" {
		t.Errorf("announcement = %q", announced)
	}
	if t1After != "2" || t2After != "1" || t2Dot != "waiting" {
		t.Errorf("rail after: t1=%q t2=%q t2 dot=%q", t1After, t2After, t2Dot)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
