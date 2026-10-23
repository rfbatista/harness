package llm_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/application/usage/usagetest"
	"operators-mcp/internal/ports"
)

// The live call log through the real /llm/ mount (ticket 05): calls go to a
// fake upstream, and a live call viewer reads them as SSE.

type liveEvt struct {
	Name string
	Data map[string]any
}

func (ev liveEvt) raw() string { b, _ := json.Marshal(ev.Data); return string(b) }

type viewer struct {
	t      *testing.T
	events chan liveEvt
	cancel context.CancelFunc
}

// openViewer opens a project's live call stream and waits for ready.
func openViewer(t *testing.T, srv *httptest.Server, project string, header http.Header) *viewer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/projects/"+project+"/usage/live", nil)
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("live stream status %d", resp.StatusCode)
	}
	v := &viewer{t: t, events: make(chan liveEvt, 1000), cancel: cancel}
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 16<<20)
		var name string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				var m map[string]any
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m)
				v.events <- liveEvt{name, m}
			}
		}
	}()
	t.Cleanup(cancel)
	if ev := v.next(); ev.Name != "ready" {
		t.Fatalf("first event = %+v, want ready", ev)
	}
	return v
}

func (v *viewer) next() liveEvt {
	v.t.Helper()
	select {
	case ev := <-v.events:
		return ev
	case <-time.After(5 * time.Second):
		v.t.Fatal("no event within 5s")
		return liveEvt{}
	}
}

func (v *viewer) nothingMore() {
	v.t.Helper()
	select {
	case ev := <-v.events:
		v.t.Fatalf("unexpected event %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func waitUntil(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(what)
}

// scriptedUpstream answers every request with the given handler.
func scriptedUpstream(t *testing.T, h http.HandlerFunc) *upstream {
	u := &upstream{Server: httptest.NewServer(h)}
	t.Cleanup(u.Close)
	return u
}

func gzipped(s string) string {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

func ptr(v any) *float64 {
	if f, ok := v.(float64); ok {
		return &f
	}
	return nil
}

func TestACallThroughTheProxyReachesAViewerAsStartedThenFinishedAndTheClientIsUntouched(t *testing.T) {
	up := scriptedUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=upstream-secret")
		_, _ = io.WriteString(w, jsonBody)
	})
	srv, e := harness(t, up)
	v := openViewer(t, srv, e.ProjectA, nil)

	body := `{"model":"claude-sonnet-4-5","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	resp, got := callClaude(t, srv, e.SessionA, body)
	if resp.StatusCode != 200 || got != jsonBody || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("client saw %d %q %q, want the upstream's unchanged", resp.StatusCode, resp.Header.Get("Content-Type"), got)
	}

	started := v.next()
	d := started.Data
	if started.Name != "call.started" || d["attribution"] != e.SessionA || d["project_id"] != e.ProjectA || d["kind"] != "session" {
		t.Fatalf("started = %+v", started)
	}
	if d["session"].(map[string]any)["id"] != e.SessionA || d["task"].(map[string]any)["id"] != e.TaskA {
		t.Fatalf("started session/task = %+v / %+v", d["session"], d["task"])
	}
	req := d["request"].(map[string]any)
	if req["method"] != "POST" || req["path"] != "/v1/messages" || req["query"] != "beta=true" {
		t.Fatalf("request = %+v", req)
	}
	if req["body"].(map[string]any)["model"] != "claude-sonnet-4-5" {
		t.Fatalf("request body = %+v", req["body"])
	}
	h := req["headers"].(map[string]any)
	if h["Authorization"].([]any)[0] != "[redacted]" || h["Anthropic-Version"].([]any)[0] != "2023-06-01" {
		t.Fatalf("request headers = %+v, want the bearer token redacted", h)
	}

	finished := v.next()
	f := finished.Data
	if finished.Name != "call.finished" || f["id"] != d["id"] || f["status"] != float64(200) || f["streamed"] != false ||
		f["model"] != "claude-sonnet-4-5-20250929" || f["error"] != "" {
		t.Fatalf("finished = %+v", finished)
	}
	u := f["usage"].(map[string]any)
	if u["input_tokens"] != float64(1200) || u["output_tokens"] != float64(340) || !near(ptr(u["estimated_cost_usd"]), 0.05145) {
		t.Fatalf("usage = %+v", u)
	}
	fr := f["response"].(map[string]any)
	if fr["body"].(map[string]any)["id"] != "msg_1" || fr["events"] != nil {
		t.Fatalf("response = %+v", fr)
	}
	if fr["headers"].(map[string]any)["Set-Cookie"].([]any)[0] != "[redacted]" {
		t.Fatalf("response headers = %+v, want Set-Cookie redacted", fr["headers"])
	}
	if strings.Contains(started.raw()+finished.raw(), "oauth-token") || strings.Contains(finished.raw(), "upstream-secret") {
		t.Fatal("a credential reached the stream")
	}

	// The usage record is what it is without a viewer.
	list := recent(t, srv, e.ProjectA, 1)
	if len(list) != 1 || list[0].SessionID != e.SessionA || list[0].InputTokens != 1200 || list[0].OutputTokens != 340 || list[0].Status != 200 {
		t.Fatalf("records = %+v", list)
	}
}

func TestAStreamedUpstreamResponseArrivesAsOrderedEventsAndTheClientGetsTheRawStream(t *testing.T) {
	up := newUpstream(t)
	srv, e := harness(t, up)
	v := openViewer(t, srv, e.ProjectA, nil)

	resp, got := callClaude(t, srv, e.SessionA, `{"model":"claude-opus-5-5","stream":true,"messages":[]}`)
	if resp.StatusCode != 200 || got != sseBody {
		t.Fatalf("client saw %d %q, want the upstream's events unchanged", resp.StatusCode, got)
	}
	v.next()
	f := v.next().Data
	if f["streamed"] != true {
		t.Fatalf("finished = %+v", f)
	}
	r := f["response"].(map[string]any)
	events := r["events"].([]any)
	if r["body"] != nil || len(events) != 3 {
		t.Fatalf("response = %+v", r)
	}
	first := events[0].(map[string]any)
	if first["event"] != "message_start" || first["data"].(map[string]any)["message"].(map[string]any)["id"] != "msg_2" ||
		events[1].(map[string]any)["event"] != "message_delta" || events[2].(map[string]any)["event"] != "message_stop" {
		t.Fatalf("events = %+v", events)
	}
}

func TestAGzipUpstreamResponseIsDecodedByItsContentEncodingWhileTheClientGetsTheGzipBytes(t *testing.T) {
	up := scriptedUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = io.WriteString(w, gzipped(sseBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = io.WriteString(w, gzipped(jsonBody))
	})
	srv, e := harness(t, up)
	v := openViewer(t, srv, e.ProjectA, nil)

	for _, tc := range []struct {
		accept, plain string
		streamed      bool
	}{{"application/json", jsonBody, false}, {"text/event-stream", sseBody, true}} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/llm/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5"}`))
		req.Header.Set("X-Llm-Attribution", e.SessionA)
		req.Header.Set("Accept", tc.accept)
		// Setting Accept-Encoding by hand keeps the transport from decoding.
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(raw) != gzipped(tc.plain) || resp.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("client saw %q encoding %q, want the upstream's gzip bytes unchanged", raw, resp.Header.Get("Content-Encoding"))
		}
		v.next()
		r := v.next().Data["response"].(map[string]any)
		if tc.streamed {
			events := r["events"].([]any)
			if len(events) != 3 || events[0].(map[string]any)["data"].(map[string]any)["type"] != "message_start" {
				t.Fatalf("gzip streamed events = %+v", r)
			}
		} else if r["body"].(map[string]any)["id"] != "msg_1" {
			t.Fatalf("gzip body = %+v", r)
		}
	}
}

func TestAViewerOfAProjectSeesItsCallsAndOneShotsNeverAnotherProjects(t *testing.T) {
	up := newUpstream(t)
	srv, e := harness(t, up)
	a := openViewer(t, srv, e.ProjectA, nil)

	callClaude(t, srv, e.SessionB, `{"model":"m"}`)
	callClaude(t, srv, "harness:document-assistant:"+e.ProjectB, `{"model":"m"}`)
	callClaude(t, srv, ports.UsageSystemAttribution, `{"model":"m"}`)
	callClaude(t, srv, "harness:document-assistant:"+e.ProjectA, `{"model":"m"}`)
	callClaude(t, srv, e.SessionA, `{"model":"m"}`)

	got := map[string]string{}
	for i := 0; i < 6; i++ {
		ev := a.next()
		if ev.Name == "call.started" {
			got[ev.Data["kind"].(string)] = ev.Data["attribution"].(string)
			if p := ev.Data["project_id"]; p != nil && p != e.ProjectA {
				t.Fatalf("another project's call = %+v", ev.Data)
			}
		}
	}
	if len(got) != 3 || got["system"] != ports.UsageSystemAttribution || got["session"] != e.SessionA || got["document-assistant"] != "harness:document-assistant:"+e.ProjectA {
		t.Fatalf("project A's viewer saw %+v, want its session, its document assistant and the one-shot", got)
	}
	a.nothingMore()
}

// spyCapture counts what the proxy hands to the capture.
type spyCapture struct {
	ports.CallCapture
	enabled, started, finished atomic.Int32
	panics                     bool
}

func (s *spyCapture) Enabled() bool {
	s.enabled.Add(1)
	return s.CallCapture.Enabled()
}

func (s *spyCapture) Started(ctx context.Context, r ports.CapturedRequest) {
	s.started.Add(1)
	s.CallCapture.Started(ctx, r)
	if s.panics {
		panic("capture failed")
	}
}

func (s *spyCapture) Finished(ctx context.Context, c ports.CapturedCall) {
	s.finished.Add(1)
	s.CallCapture.Finished(ctx, c)
	if s.panics {
		panic("capture failed")
	}
}

func TestCaptureIsOnOnlyWhileAViewerIsOpenAndNothingIsCopiedOtherwise(t *testing.T) {
	up := newUpstream(t)
	var spy *spyCapture
	srv, e := harnessWith(t, up, func(c ports.CallCapture) ports.CallCapture {
		spy = &spyCapture{CallCapture: c}
		return spy
	}, nil)

	callClaude(t, srv, e.SessionA, `{"model":"m"}`)
	if spy.enabled.Load() == 0 || spy.started.Load() != 0 || spy.finished.Load() != 0 {
		t.Fatalf("with no viewer: asked %d, started %d, finished %d; want asked and nothing handed over", spy.enabled.Load(), spy.started.Load(), spy.finished.Load())
	}

	v := openViewer(t, srv, e.ProjectA, nil)
	callClaude(t, srv, e.SessionA, `{"model":"m"}`)
	if v.next().Name != "call.started" || v.next().Name != "call.finished" {
		t.Fatal("the open viewer did not get the call")
	}

	v.cancel()
	waitUntil(t, func() bool { return !e.Service.Enabled() }, "capture still on after the viewer closed")
	started := spy.started.Load()
	callClaude(t, srv, e.SessionA, `{"model":"m"}`)
	if spy.started.Load() != started {
		t.Fatal("a call was handed to the capture after the last viewer closed")
	}
	// Usage records are written either way.
	if list := recent(t, srv, e.ProjectA, 3); len(list) != 3 {
		t.Fatalf("records = %d, want 3", len(list))
	}
}

func TestAFailingCaptureNeverFailsTheCallOrItsUsageRecord(t *testing.T) {
	up := newUpstream(t)
	srv, e := harnessWith(t, up, func(c ports.CallCapture) ports.CallCapture {
		return &spyCapture{CallCapture: c, panics: true}
	}, nil)
	openViewer(t, srv, e.ProjectA, nil)

	resp, got := callClaude(t, srv, e.SessionA, `{"model":"claude-sonnet-4-5"}`)
	if resp.StatusCode != 200 || got != jsonBody {
		t.Fatalf("client saw %d %q, want the upstream's unchanged", resp.StatusCode, got)
	}
	if list := recent(t, srv, e.ProjectA, 1); len(list) != 1 || list[0].InputTokens != 1200 {
		t.Fatalf("records = %+v, want the call's usage record", list)
	}
}

// gatedWriter is a response writer whose flushes wait for the test: a viewer
// that does not read.
type gatedWriter struct {
	*httptest.ResponseRecorder
	gate chan struct{}
	mu   sync.Mutex
}

func (w *gatedWriter) Flush() {
	<-w.gate
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ResponseRecorder.Flush()
}

func (w *gatedWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseRecorder.Write(b)
}

func (w *gatedWriter) body() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseRecorder.Body.String()
}

func TestAViewerThatDoesNotReadGetsAMissedMarkerAndNeverSlowsOrFailsTheCalls(t *testing.T) {
	up := newUpstream(t)
	srv, e := harnessWith(t, up, nil, nil, usagetest.WithLiveCallBuffer(4))
	router := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Usage: e.Service, LiveCalls: e.Service}))

	ctx, cancel := context.WithCancel(context.Background())
	w := &gatedWriter{ResponseRecorder: httptest.NewRecorder(), gate: make(chan struct{})}
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+e.ProjectA+"/usage/live", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { router.ServeHTTP(w, req); close(done) }()
	waitUntil(t, e.Service.Enabled, "viewer connected")

	start := time.Now()
	for i := 0; i < 12; i++ {
		resp, got := callClaude(t, srv, e.SessionA, `{"model":"claude-sonnet-4-5"}`)
		if resp.StatusCode != 200 || got != jsonBody {
			t.Fatalf("call %d = %d %q, want the upstream's unchanged", i, resp.StatusCode, got)
		}
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("12 calls took %v with a stuck viewer", time.Since(start))
	}

	close(w.gate)
	waitUntil(t, func() bool { return strings.Contains(w.body(), "event: calls.missed") }, "no missed marker")
	cancel()
	<-done
	if list := recent(t, srv, e.ProjectA, 12); len(list) != 12 {
		t.Fatalf("records = %d, want all 12", len(list))
	}
}

func TestTheLiveStreamNeedsTheTokenButTheProxyDoesNot(t *testing.T) {
	up := newUpstream(t)
	srv, e := harnessWith(t, up, nil, []httpapi.Option{httpapi.WithToken("s3cret")})

	resp, err := http.Get(srv.URL + "/api/projects/" + e.ProjectA + "/usage/live")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("live stream without a token = %d, want 401", resp.StatusCode)
	}
	if e.Service.Enabled() {
		t.Fatal("an unauthenticated request turned capture on")
	}
	v := openViewer(t, srv, e.ProjectA, http.Header{"Authorization": {"Bearer s3cret"}})
	if r, got := callClaude(t, srv, e.SessionA, `{"model":"m"}`); r.StatusCode != 200 || got != jsonBody {
		t.Fatalf("the proxy answered %d without the token", r.StatusCode)
	}
	if v.next().Name != "call.started" {
		t.Fatal("the authenticated viewer got no call")
	}
}
