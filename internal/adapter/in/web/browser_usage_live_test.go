package web

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// liveStub stands in for GET /api/projects/p1/usage/live: it names its events
// like the real one, sends `ready`, then what the test pushes.
type liveStub struct {
	opened, closed atomic.Int32
	push           chan string // one whole SSE frame each
}

func (l *liveStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)
	l.opened.Add(1)
	defer l.closed.Add(1)
	fmt.Fprint(w, "event: ready\ndata: {\"project_id\":\"p1\"}\n\n")
	flusher.Flush()
	for {
		select {
		case frame := <-l.push:
			fmt.Fprint(w, frame)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// TestUsageLiveViewOpensStoppedStreamsOnStartAndClosesOnStop is the Live view's
// smoke test in a real browser against a stubbed stream: stopped and with no
// stream until Start, a call appears in flight and completes in place, a row
// opens into its tabs and JSON tree, and Stop releases the connection. The
// whole flow through the proxy is ticket 05's browser test.
func TestUsageLiveViewOpensStoppedStreamsOnStartAndClosesOnStop(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{},
		Sessions:     fakeSessions{},
		Usage:        fakeUsage{},
		Now:          func() time.Time { return now },
	}, assets, nil)
	stub := &liveStub{push: make(chan string, 8)}
	mux := http.NewServeMux()
	mux.Handle("/api/projects/p1/usage/live", stub)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	started := `event: call.started
data: {"id":"c1","attribution":"s1","project_id":"p1","session":{"id":"s1","title":"Port the parser"},"task":{"id":"t-feed","title":"Add SSE feed"},"kind":"session","started_at":"2026-01-02T03:04:05.000Z","request":{"method":"POST","path":"/v1/messages","query":"","headers":{"x-api-key":["[redacted]"]},"body":{"model":"m","messages":[{"role":"user","content":"Hello there"}]}}}

`
	finished := `event: call.finished
data: {"id":"c1","ended_at":"2026-01-02T03:04:06.234Z","duration_ms":1234,"status":200,"streamed":true,"model":"claude-sonnet-4-5","error":"","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"estimated_cost_usd":0.01},"response":{"headers":{},"body":null,"events":[{"event":"message_start","data":{"type":"message_start","message":{"id":"m1","model":"claude-sonnet-4-5","role":"assistant","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}},{"event":"content_block_start","data":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}},{"event":"content_block_delta","data":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi there"}}}]}}

`
	text := func(id string) string {
		return fmt.Sprintf(`(document.querySelector('[data-testid=%s]')?.textContent ?? '').replace(/\s+/g, ' ').trim()`, id)
	}
	count := func(id string) string {
		return fmt.Sprintf(`document.querySelectorAll('[data-testid=%s]').length`, id)
	}
	click := func(id string) chromedp.Action {
		return chromedp.Evaluate(fmt.Sprintf(`document.querySelector('[data-testid=%s]').click()`, id), nil)
	}
	poll := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	var indicator, toggle, summary, treeText string
	var callsBefore, spinnersWhileRunning, spinnersAfter, foldsOpen, foldsAfterCollapse, foldsAfterExpand float64
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/usage"),
		// Alpine has taken over once the indicator reads Stopped.
		poll(text("live-indicator")+` === 'Stopped'`),
		chromedp.Evaluate(text("live-indicator"), &indicator),
		chromedp.Evaluate(count("live-call"), &callsBefore),

		click("live-toggle"),
		poll(text("live-indicator")+` === 'Live'`),
		chromedp.Evaluate(text("live-toggle"), &toggle),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if indicator != "Stopped" || callsBefore != 0 || toggle != "Stop" {
		t.Errorf("indicator %q, calls %v, toggle %q after Start", indicator, callsBefore, toggle)
	}
	if got := stub.opened.Load(); got != 1 {
		t.Fatalf("the stream was opened %d times by Start, want 1", got)
	}

	stub.push <- started
	err = chromedp.Run(ctx,
		poll(count("live-call")+` === 1`),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-testid=live-spinner]')].filter((e) => e.checkVisibility()).length`, &spinnersWhileRunning),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	stub.push <- finished
	err = chromedp.Run(ctx,
		poll(text("live-call-summary")+` .includes('200')`),
		poll(`[...document.querySelectorAll('[data-testid=live-spinner]')].filter((e) => e.checkVisibility()).length === 0`),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-testid=live-spinner]')].filter((e) => e.checkVisibility()).length`, &spinnersAfter),
		chromedp.Evaluate(`document.querySelector('[data-testid=live-call]').textContent.replace(/\s+/g, ' ')`, &summary),
		click("live-call-summary"),
		poll(count("live-call-body")+` === 1`),
		poll(text("live-tree")+` .includes('Hello there')`),
		chromedp.Evaluate(count("live-tree")+` && document.querySelectorAll('[data-testid=live-tree] details').length`, &foldsOpen),
		click("live-collapse-all"),
		chromedp.Evaluate(`document.querySelectorAll('[data-testid=live-tree] details[open]').length`, &foldsAfterCollapse),
		click("live-expand-all"),
		chromedp.Evaluate(`document.querySelectorAll('[data-testid=live-tree] details:not([open])').length`, &foldsAfterExpand),
		click("live-tab-message"),
		poll(text("live-tree")+` .includes('Hi there')`),
		chromedp.Evaluate(text("live-tree"), &treeText),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if spinnersWhileRunning != 1 || spinnersAfter != 0 {
		t.Errorf("in-flight spinners: %v while running, %v after it finished, want 1 and 0", spinnersWhileRunning, spinnersAfter)
	}
	for _, want := range []string{"Port the parser", "Add SSE feed", "POST /v1/messages", "claude-sonnet-4-5", "200", "15", "1.2s"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the row %q does not show %q", summary, want)
		}
	}
	if foldsOpen < 2 || foldsAfterCollapse != 0 || foldsAfterExpand != 0 {
		t.Errorf("tree folds: %v at first, %v open after collapse all, %v closed after expand all", foldsOpen, foldsAfterCollapse, foldsAfterExpand)
	}
	if !strings.Contains(treeText, `"Hi there"`) {
		t.Errorf("the Message tab shows %q, want the reassembled text", treeText)
	}

	// Stop closes the stream and releases the connection.
	err = chromedp.Run(ctx,
		click("live-toggle"),
		poll(text("live-indicator")+` === 'Stopped'`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	deadline := time.Now().Add(5 * time.Second)
	for stub.closed.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := stub.closed.Load(); got != 1 {
		t.Errorf("after Stop the server saw %d closed streams, want 1", got)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors on the page:\n%s", strings.Join(e, "\n"))
	}
}
