package llm_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/llm"
	"operators-mcp/internal/application/usage/usagetest"
	"operators-mcp/internal/ports"
)

// The LLM proxy at /llm/ in-process (seam H1): requests go through to a fake
// upstream, and what they used is read back through the usage API and its
// stream.

const jsonBody = `{"id":"msg_1","type":"message","model":"claude-sonnet-4-5-20250929","content":[{"type":"text","text":"hi"}],` +
	`"usage":{"input_tokens":1200,"output_tokens":340,"cache_creation_input_tokens":5000,"cache_read_input_tokens":80000}}`

const sseBody = "event: message_start\n" +
	`data: {"type":"message_start","message":{"id":"msg_2","model":"claude-opus-5-5","usage":{"input_tokens":1000000,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":100000}}` + "\n\n" +
	"event: message_stop\n" + `data: {"type":"message_stop"}` + "\n\n"

// upstream is a fake Anthropic API that remembers what reached it.
type upstream struct {
	*httptest.Server
	mu   sync.Mutex
	seen []seenRequest
}

type seenRequest struct {
	path   string
	header http.Header
	body   string
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.seen = append(u.seen, seenRequest{r.URL.Path, r.Header.Clone(), string(body)})
		u.mu.Unlock()
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, sseBody)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonBody)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) requests() []seenRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]seenRequest(nil), u.seen...)
}

// harness serves /llm/ and /api/ the way the server mounts them.
func harness(t *testing.T, up *upstream) (*httptest.Server, *usagetest.Env) {
	t.Helper()
	return harnessWith(t, up, nil, nil)
}

// harnessWith is harness with the live call capture wired the way the server
// wires it (a nil wrap passes the usage service as it is), a router option
// and fixture options.
func harnessWith(t *testing.T, up *upstream, wrap func(ports.CallCapture) ports.CallCapture, routerOpts []httpapi.Option, opts ...usagetest.Option) (*httptest.Server, *usagetest.Env) {
	t.Helper()
	e := usagetest.NewEnv(t, opts...)
	var capture ports.CallCapture = e.Service
	if wrap != nil {
		capture = wrap(capture)
	}
	proxy, err := llm.Handler(llm.Config{Upstream: up.URL, Sink: e.Service, Capture: capture})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(llm.PathPrefix, proxy)
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Usage: e.Service, LiveCalls: e.Service}), routerOpts...))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, e
}

func callClaude(t *testing.T, srv *httptest.Server, attribution, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/llm/v1/messages?beta=true", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer oauth-token")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	if attribution != "" {
		req.Header.Set("X-Llm-Attribution", attribution)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	return resp, string(got)
}

type record struct {
	SessionID                string   `json:"session_id"`
	TaskID                   string   `json:"task_id"`
	ProjectID                string   `json:"project_id"`
	Attribution              string   `json:"attribution"`
	Model                    string   `json:"model"`
	Path                     string   `json:"path"`
	Status                   int      `json:"status"`
	Streamed                 bool     `json:"streamed"`
	InputTokens              int64    `json:"input_tokens"`
	OutputTokens             int64    `json:"output_tokens"`
	CacheCreationInputTokens int64    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64    `json:"cache_read_input_tokens"`
	EstimatedCostUSD         *float64 `json:"estimated_cost_usd"`
}

// follow answers the project's usage stream, one record per data: line.
func follow(t *testing.T, ctx context.Context, srv *httptest.Server, project string) <-chan record {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/projects/"+project+"/usage/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan record, 8)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var r record
				_ = json.Unmarshal([]byte(data), &r)
				out <- r
			}
		}
	}()
	return out
}

func next(t *testing.T, ctx context.Context, ch <-chan record) record {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-ctx.Done():
		t.Fatal("no usage record arrived")
		return record{}
	}
}

// recent polls the recent-records endpoint until it holds n records: the
// proxy hands a record to the sink after the response is written.
func recent(t *testing.T, srv *httptest.Server, project string, n int) []record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(srv.URL + "/api/projects/" + project + "/usage/records")
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Records []record `json:"records"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if len(page.Records) >= n || time.Now().After(deadline) {
			return page.Records
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func near(a *float64, want float64) bool { return a != nil && *a-want < 1e-9 && want-*a < 1e-9 }

func TestAProxiedCallPassesThroughUntouchedAndIsRecordedForItsSession(t *testing.T) {
	up := newUpstream(t)
	srv, e := harness(t, up)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	live := follow(t, ctx, srv, e.ProjectA)

	body := `{"model":"claude-sonnet-4-5","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	resp, got := callClaude(t, srv, e.SessionA, body)
	if resp.StatusCode != 200 || got != jsonBody || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %q %q, want the upstream's unchanged", resp.StatusCode, resp.Header.Get("Content-Type"), got)
	}
	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("upstream saw %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.path != "/v1/messages" || r.body != body || r.header.Get("Authorization") != "Bearer oauth-token" || r.header.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("upstream saw %s %q auth %q version %q, want the request unchanged at /v1/messages", r.path, r.body, r.header.Get("Authorization"), r.header.Get("anthropic-version"))
	}
	if v := r.header.Get("X-Llm-Attribution"); v != "" {
		t.Errorf("upstream saw the attribution header %q, want it stripped", v)
	}

	ev := next(t, ctx, live)
	// 1200×$3 + 340×$15 + 5000×$3.75 + 80000×$0.30 per million.
	if ev.SessionID != e.SessionA || ev.TaskID != e.TaskA || ev.ProjectID != e.ProjectA || ev.Model != "claude-sonnet-4-5-20250929" ||
		ev.InputTokens != 1200 || ev.OutputTokens != 340 || ev.CacheCreationInputTokens != 5000 || ev.CacheReadInputTokens != 80000 ||
		ev.Status != 200 || ev.Streamed || ev.Path != "/v1/messages" || !near(ev.EstimatedCostUSD, 0.05145) {
		t.Errorf("live record = %+v, want session A's, with its four token counts and $0.05145 estimated", ev)
	}
	if list := recent(t, srv, e.ProjectA, 1); len(list) != 1 || list[0].SessionID != e.SessionA {
		t.Errorf("recent records = %+v, want the one call", list)
	}
}

func TestAStreamedCallIsCountedFromItsEvents(t *testing.T) {
	up := newUpstream(t)
	srv, e := harness(t, up)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	live := follow(t, ctx, srv, e.ProjectA)

	resp, got := callClaude(t, srv, e.SessionA, `{"model":"claude-opus-5-5","stream":true,"messages":[]}`)
	if resp.StatusCode != 200 || got != sseBody {
		t.Fatalf("streamed response = %d %q, want the upstream's events unchanged", resp.StatusCode, got)
	}
	ev := next(t, ctx, live)
	// $4 per million in, $20 per million out.
	if !ev.Streamed || ev.Model != "claude-opus-5-5" || ev.InputTokens != 1_000_000 || ev.OutputTokens != 100_000 || !near(ev.EstimatedCostUSD, 6.0) {
		t.Errorf("live record = %+v, want streamed, 1M in, 100K out, $6 estimated", ev)
	}
}

func TestTheHarnessesOwnCallsAreRecordedAsItsOwnSpend(t *testing.T) {
	up := newUpstream(t)
	srv, e := harness(t, up)
	callClaude(t, srv, ports.UsageSystemAttribution, `{"model":"claude-sonnet-4-5"}`)
	list := recent(t, srv, ports.UsageSystemProject, 1)
	if len(list) != 1 || list[0].Attribution != ports.UsageSystemAttribution || list[0].ProjectID != "" || list[0].SessionID != "" {
		t.Fatalf("the harness's records = %+v, want the one-shot, attributed to no project", list)
	}
	if other := recent(t, srv, e.ProjectA, 0); len(other) != 0 {
		t.Errorf("project A has %+v, want nothing", other)
	}
}

func TestAProxiedCallSucceedsWhenItsUsageCannotBeStored(t *testing.T) {
	up := newUpstream(t)
	srv, e := harness(t, up)
	if err := e.DB.Exec("DROP TABLE usage_records").Error; err != nil {
		t.Fatal(err)
	}
	resp, got := callClaude(t, srv, e.SessionA, `{"model":"claude-sonnet-4-5"}`)
	if resp.StatusCode != 200 || got != jsonBody {
		t.Fatalf("response with the store failing = %d %q, want the upstream's unchanged", resp.StatusCode, got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(e.Log.String(), "usage record not stored") {
		if time.Now().After(deadline) {
			t.Fatalf("log = %q, want the failed usage record logged", e.Log.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOnlyLoopbackPeersReachTheProxy(t *testing.T) {
	up := newUpstream(t)
	e := usagetest.NewEnv(t)
	proxy, err := llm.Handler(llm.Config{Upstream: up.URL, Sink: e.Service})
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []string{"192.0.2.10:5555", "[2001:db8::1]:5555", "garbage"} {
		req := httptest.NewRequest(http.MethodPost, "/llm/v1/messages", strings.NewReader(`{}`))
		req.RemoteAddr = peer
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("peer %s = %d, want 403", peer, rec.Code)
		}
	}
	if n := len(up.requests()); n != 0 {
		t.Errorf("upstream saw %d requests from non-loopback peers, want none", n)
	}
	for _, peer := range []string{"127.0.0.1:5555", "[::1]:5555"} {
		req := httptest.NewRequest(http.MethodPost, "/llm/v1/messages", strings.NewReader(`{}`))
		req.RemoteAddr = peer
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("loopback peer %s = %d, want 200", peer, rec.Code)
		}
	}
}
