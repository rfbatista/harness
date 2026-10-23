package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/llm"
	"operators-mcp/internal/application/usage/usagetest"
	"operators-mcp/internal/domain"
)

// TestLiveCallLogShowsACallMadeThroughTheProxyOnlyWhileStarted is ticket 05's
// browser test: the real server pieces (the page, the API with its live call
// stream, the /llm/ proxy mount) against a fake upstream. Start, a call made
// through the proxy appears and opens, Stop, and a further call adds nothing.
func TestLiveCallLogShowsACallMadeThroughTheProxyOnlyWhileStarted(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","model":"claude-sonnet-4-5-20250929","content":[{"type":"text","text":"Greetings from upstream"}],"usage":{"input_tokens":12,"output_tokens":7}}`)
	}))
	defer upstream.Close()

	e := usagetest.NewEnv(t)
	proxy, err := llm.Handler(llm.Config{Upstream: upstream.URL, Sink: e.Service, Capture: e.Service})
	if err != nil {
		t.Fatal(err)
	}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: e.ProjectA, Name: "Alpha"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: e.TaskA, ProjectID: e.ProjectA, Title: "Build it", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{},
		Sessions:     fakeSessions{},
		Usage:        e.Service,
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle(llm.PathPrefix, proxy)
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Usage: e.Service, LiveCalls: e.Service})))
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	callProxy := func(prompt string) {
		t.Helper()
		body := fmt.Sprintf(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":%q}]}`, prompt)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/llm/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Llm-Attribution", e.SessionA)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(got), "Greetings from upstream") {
			t.Fatalf("the proxied call answered %d %q", resp.StatusCode, got)
		}
	}
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

	// Started: a call through the proxy appears as an entry, completed in
	// place, and opens into its request and response.
	var summary, tree string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/"+e.ProjectA+"/usage"),
		poll(text("live-indicator")+` === 'Stopped'`),
		click("live-toggle"),
		poll(text("live-indicator")+` === 'Live'`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	eventually(t, e.Service.Enabled, "capture is not on after Start")

	callProxy("First question")
	err = chromedp.Run(ctx,
		poll(count("live-call")+` === 1`),
		poll(text("live-call-summary")+` .includes('200')`),
		chromedp.Evaluate(text("live-call"), &summary),
		click("live-call-summary"),
		poll(count("live-call-body")+` === 1`),
		poll(text("live-tree")+` .includes('First question')`),
		chromedp.Evaluate(text("live-tree"), &tree),
		click("live-tab-response"),
		poll(text("live-tree")+` .includes('Greetings from upstream')`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for _, want := range []string{"POST /v1/messages", "claude-sonnet-4-5", "200"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the entry %q does not show %q", summary, want)
		}
	}
	if !strings.Contains(tree, "First question") {
		t.Errorf("the Request tab shows %q, want the request body", tree)
	}

	// Searched: a second call arrives while live, and a word found only in
	// the first call's request narrows the log to it, highlighted.
	callProxy("Second question")
	err = chromedp.Run(ctx,
		poll(count("live-call")+` === 2`),
		click("live-tab-request"),
		chromedp.Evaluate(`(() => { const i = document.querySelector('[data-testid=live-search]'); i.value = 'first question'; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		poll(count("live-call")+` === 1`),
		poll(text("live-count")+` === '1 of 2 calls'`),
		poll(`document.querySelectorAll('[data-testid=live-tree] mark.json-match').length > 0`),
		chromedp.Evaluate(`(() => { const i = document.querySelector('[data-testid=live-search]'); i.value = ''; i.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		poll(count("live-call")+` === 2`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}

	// Stopped: the stream closes, capture goes off, and a further call adds
	// no entry.
	err = chromedp.Run(ctx,
		click("live-toggle"),
		poll(text("live-indicator")+` === 'Stopped'`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	eventually(t, func() bool { return !e.Service.Enabled() }, "capture is still on after Stop")
	callProxy("Third question")
	time.Sleep(500 * time.Millisecond)
	var after float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(count("live-call"), &after)); err != nil {
		t.Fatal(err)
	}
	if after != 2 {
		t.Errorf("%v entries after Stop and a further call, want still 2", after)
	}
	if es := errs.all(); len(es) > 0 {
		t.Errorf("JavaScript errors on the page:\n%s", strings.Join(es, "\n"))
	}
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(what)
}
