package httpapi

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/application/usage/usagetest"
	"operators-mcp/internal/ports"
)

// The live call stream at the server HTTP seam: calls are fed to the fan-out
// directly (as the LLM proxy will) and read back as SSE.

type liveEvt struct {
	Name string
	Data map[string]any
}

type liveReader struct {
	t      *testing.T
	events chan liveEvt
	cancel context.CancelFunc
}

// openLive connects to a project's live call stream and waits for ready.
func openLive(t *testing.T, srv *httptest.Server, project string, header http.Header) *liveReader {
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
		t.Fatalf("status %d", resp.StatusCode)
	}
	r := &liveReader{t: t, events: make(chan liveEvt, 1000), cancel: cancel}
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
				r.events <- liveEvt{name, m}
			}
		}
	}()
	t.Cleanup(cancel)
	if ev := r.next(); ev.Name != "ready" || ev.Data["project_id"] != project {
		t.Fatalf("first event = %+v, want ready for %s", ev, project)
	}
	return r
}

func (r *liveReader) next() liveEvt {
	r.t.Helper()
	select {
	case ev := <-r.events:
		return ev
	case <-time.After(5 * time.Second):
		r.t.Fatal("no event within 5s")
		return liveEvt{}
	}
}

func (r *liveReader) nothingMore() {
	r.t.Helper()
	select {
	case ev := <-r.events:
		r.t.Fatalf("unexpected event %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

func liveServer(t *testing.T, opts ...usagetest.Option) (*httptest.Server, *usagetest.Env) {
	t.Helper()
	e := usagetest.NewEnv(t, opts...)
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Usage: e.Service, LiveCalls: e.Service})))
	t.Cleanup(srv.Close)
	return srv, e
}

var liveT0 = time.Date(2026, 10, 14, 10, 0, 0, 123_000_000, time.UTC)

func startReq(id, attribution string, body string) ports.CapturedRequest {
	return ports.CapturedRequest{
		ID: id, Attribution: attribution, Method: "POST", Path: "/v1/messages", Query: "beta=true",
		Header:    http.Header{"Content-Type": {"application/json"}, "X-Api-Key": {"sk-secret"}, "Authorization": {"Bearer secret"}},
		Body:      []byte(body),
		StartedAt: liveT0,
	}
}

func finishCall(id string, body string, streamed bool, hdr http.Header) ports.CapturedCall {
	return ports.CapturedCall{
		ID: id, Status: 200, Header: hdr, Body: []byte(body), Streamed: streamed,
		Usage: ports.UsageRequest{ID: id, Model: "claude-sonnet-5", StartedAt: liveT0, EndedAt: liveT0.Add(1500 * time.Millisecond),
			InputTokens: 1_000_000, OutputTokens: 1_000_000},
	}
}

func TestLiveStreamSendsReadyThenOnlyCallsThatStartAfterItOpened(t *testing.T) {
	srv, e := liveServer(t)
	e.Service.Started(context.Background(), startReq("early", e.SessionA, `{}`))
	e.Service.Finished(context.Background(), finishCall("early", `{}`, false, nil))

	r := openLive(t, srv, e.ProjectA, nil)
	r.nothingMore()

	e.Service.Started(context.Background(), startReq("c1", e.SessionA, `{"model":"claude-sonnet-5"}`))
	ev := r.next()
	if ev.Name != "call.started" || ev.Data["id"] != "c1" {
		t.Fatalf("got %+v", ev)
	}
}

func TestLiveStreamDeliversTheProjectsCallsAndCallsOfNoProjectNeverAnotherProjects(t *testing.T) {
	srv, e := liveServer(t)
	r := openLive(t, srv, e.ProjectA, nil)
	ctx := context.Background()
	e.Service.Started(ctx, startReq("b", e.SessionB, `{}`))
	e.Service.Started(ctx, startReq("sys", ports.UsageSystemAttribution, `{}`))
	e.Service.Started(ctx, startReq("a", e.SessionA, `{}`))
	e.Service.Finished(ctx, finishCall("b", `{}`, false, nil))

	sys := r.next()
	if sys.Data["id"] != "sys" || sys.Data["project_id"] != nil || sys.Data["session"] != nil || sys.Data["task"] != nil || sys.Data["kind"] != "system" {
		t.Fatalf("no-project call = %+v", sys.Data)
	}
	a := r.next()
	if a.Data["id"] != "a" || a.Data["project_id"] != e.ProjectA || a.Data["kind"] != "session" {
		t.Fatalf("project call = %+v", a.Data)
	}
	r.nothingMore() // neither b's start nor b's finish
}

func TestCallStartedCarriesTheContractFieldsAndRedactsCredentials(t *testing.T) {
	srv, e := liveServer(t)
	r := openLive(t, srv, e.ProjectA, nil)
	e.Service.Started(context.Background(), startReq("c1", e.SessionA, `{"model":"claude-sonnet-5","n":1}`))

	d := r.next().Data
	if d["id"] != "c1" || d["attribution"] != e.SessionA || d["started_at"] != "2026-10-14T10:00:00.123Z" {
		t.Fatalf("started = %+v", d)
	}
	if s := d["session"].(map[string]any); s["id"] != e.SessionA {
		t.Fatalf("session = %+v", s)
	}
	if task := d["task"].(map[string]any); task["id"] != e.TaskA || task["title"] != "Build it" {
		t.Fatalf("task = %+v", task)
	}
	req := d["request"].(map[string]any)
	if req["method"] != "POST" || req["path"] != "/v1/messages" || req["query"] != "beta=true" {
		t.Fatalf("request = %+v", req)
	}
	if body := req["body"].(map[string]any); body["n"] != float64(1) {
		t.Fatalf("body = %+v", body)
	}
	h := req["headers"].(map[string]any)
	if h["Content-Type"].([]any)[0] != "application/json" || h["X-Api-Key"].([]any)[0] != "[redacted]" || h["Authorization"].([]any)[0] != "[redacted]" {
		t.Fatalf("headers = %+v", h)
	}
}

func TestCallFinishedCarriesTheContractFieldsAndTheEstimatedCost(t *testing.T) {
	srv, e := liveServer(t)
	r := openLive(t, srv, e.ProjectA, nil)
	ctx := context.Background()
	e.Service.Started(ctx, startReq("c1", e.SessionA, `{}`))
	r.next()
	e.Service.Finished(ctx, finishCall("c1", `{"id":"msg_1"}`, false, http.Header{"Set-Cookie": {"s=1"}, "Content-Type": {"application/json"}}))

	ev := r.next()
	d := ev.Data
	if ev.Name != "call.finished" || d["id"] != "c1" || d["ended_at"] != "2026-10-14T10:00:01.623Z" || d["duration_ms"] != float64(1500) ||
		d["status"] != float64(200) || d["streamed"] != false || d["model"] != "claude-sonnet-5" || d["error"] != "" {
		t.Fatalf("finished = %+v", d)
	}
	u := d["usage"].(map[string]any)
	if u["input_tokens"] != float64(1_000_000) || u["output_tokens"] != float64(1_000_000) || u["estimated_cost_usd"] != 12.0 {
		t.Fatalf("usage = %+v", u)
	}
	resp := d["response"].(map[string]any)
	if resp["body"].(map[string]any)["id"] != "msg_1" || resp["events"] != nil {
		t.Fatalf("response = %+v", resp)
	}
	if resp["headers"].(map[string]any)["Set-Cookie"].([]any)[0] != "[redacted]" {
		t.Fatalf("response headers = %+v", resp["headers"])
	}
}

func TestAStreamedResponseArrivesAsOrderedEventsWithParsedData(t *testing.T) {
	srv, e := liveServer(t)
	r := openLive(t, srv, e.ProjectA, nil)
	ctx := context.Background()
	e.Service.Started(ctx, startReq("c1", e.SessionA, `{}`))
	r.next()
	sse := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: ping\r\ndata: not json\r\n\r\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	e.Service.Finished(ctx, finishCall("c1", sse, true, http.Header{"Content-Type": {"text/event-stream"}}))

	resp := r.next().Data["response"].(map[string]any)
	events := resp["events"].([]any)
	if len(events) != 3 || resp["body"] != nil {
		t.Fatalf("response = %+v", resp)
	}
	first, second, third := events[0].(map[string]any), events[1].(map[string]any), events[2].(map[string]any)
	if first["event"] != "message_start" || first["data"].(map[string]any)["type"] != "message_start" ||
		second["event"] != "ping" || second["data"] != "not json" || third["event"] != "message_stop" {
		t.Fatalf("events = %+v", events)
	}
}

func gz(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

func TestGzipBodiesAreDecodedByContentEncodingAndUnknownEncodingsArriveAsStrings(t *testing.T) {
	srv, e := liveServer(t)
	r := openLive(t, srv, e.ProjectA, nil)
	ctx := context.Background()

	req := startReq("c1", e.SessionA, gz(t, `{"hello":"world"}`))
	req.Header.Set("Content-Encoding", "gzip")
	e.Service.Started(ctx, req)
	if body := r.next().Data["request"].(map[string]any)["body"].(map[string]any); body["hello"] != "world" {
		t.Fatalf("gzip request body = %+v", body)
	}
	e.Service.Finished(ctx, finishCall("c1", gz(t, "event: a\ndata: {\"x\":1}\n\n"), true, http.Header{"Content-Encoding": {"gzip"}}))
	events := r.next().Data["response"].(map[string]any)["events"].([]any)
	if events[0].(map[string]any)["data"].(map[string]any)["x"] != float64(1) {
		t.Fatalf("gzip events = %+v", events)
	}

	req2 := startReq("c2", e.SessionA, "\x00br-bytes")
	req2.Header.Set("Content-Encoding", "br")
	e.Service.Started(ctx, req2)
	if body := r.next().Data["request"].(map[string]any)["body"]; body != "\x00br-bytes" {
		t.Fatalf("unknown encoding body = %#v", body)
	}
	e.Service.Finished(ctx, finishCall("c2", "zzz", false, http.Header{"Content-Encoding": {"zstd"}}))
	fin := r.next()
	if fin.Name != "call.finished" || fin.Data["response"].(map[string]any)["body"] != "zzz" {
		t.Fatalf("unknown encoding response = %+v", fin)
	}
	// An unparsable plain body is a string too.
	e.Service.Started(ctx, startReq("c3", e.SessionA, "plain text"))
	if body := r.next().Data["request"].(map[string]any)["body"]; body != "plain text" {
		t.Fatalf("plain body = %#v", body)
	}
}

func TestCaptureIsEnabledExactlyWhileAViewerIsConnected(t *testing.T) {
	srv, e := liveServer(t)
	if e.Service.Enabled() {
		t.Fatal("enabled with no viewer")
	}
	a := openLive(t, srv, e.ProjectA, nil)
	b := openLive(t, srv, e.ProjectB, nil)
	if !e.Service.Enabled() {
		t.Fatal("disabled with two viewers")
	}
	a.cancel()
	waitFor(t, func() bool { return e.Service.Enabled() }, "still enabled for the second viewer")
	b.cancel()
	waitFor(t, func() bool { return !e.Service.Enabled() }, "disabled after the last viewer closed")
}

func waitFor(t *testing.T, cond func() bool, what string) {
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

// gatedWriter is a response writer whose flushes wait for the test: a client
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

func TestAViewerThatDoesNotReadGetsAMissedMarkerStaysConnectedAndNeverBlocksTheProducer(t *testing.T) {
	e := usagetest.NewEnv(t, usagetest.WithLiveCallBuffer(4))
	router := NewRouter(NewHandler(Services{Usage: e.Service, LiveCalls: e.Service}))

	ctx, cancel := context.WithCancel(context.Background())
	w := &gatedWriter{ResponseRecorder: httptest.NewRecorder(), gate: make(chan struct{})}
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+e.ProjectA+"/usage/live", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { router.ServeHTTP(w, req); close(done) }()
	waitFor(t, e.Service.Enabled, "viewer connected")

	// The writer is stuck on its first flush; the producer must not be.
	produced := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			id := string(rune('a' + i))
			e.Service.Started(context.Background(), startReq(id, e.SessionA, `{}`))
			e.Service.Finished(context.Background(), finishCall(id, `{}`, false, nil))
		}
		close(produced)
	}()
	select {
	case <-produced:
	case <-time.After(3 * time.Second):
		t.Fatal("the producer was blocked by a viewer that does not read")
	}

	close(w.gate) // the viewer starts reading
	waitFor(t, func() bool { return strings.Contains(w.body(), "event: calls.missed") }, "no missed marker")
	cancel()
	<-done

	var delivered, missed int
	var started, finished = map[string]bool{}, map[string]bool{}
	var name string
	for _, line := range strings.Split(w.body(), "\n") {
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var d struct {
				ID    string `json:"id"`
				Count int    `json:"count"`
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d)
			switch name {
			case "call.started":
				started[d.ID] = true
				delivered++
			case "call.finished":
				if !started[d.ID] {
					t.Fatalf("finished %s without its started", d.ID)
				}
				finished[d.ID] = true
				delivered++
			case "calls.missed":
				missed += d.Count
			}
		}
	}
	// 20 calls are 40 events; each is delivered or counted, never both. A
	// finish whose start was dropped is not delivered and not counted again.
	droppedStarts := 20 - len(started)
	if delivered+missed+droppedStarts != 40 {
		t.Fatalf("delivered %d + missed %d + finishes of dropped starts %d != 40", delivered, missed, droppedStarts)
	}
	if missed == 0 {
		t.Fatal("missed count is zero")
	}
}

func TestTheLiveStreamNeedsTheSameTokenAsTheOtherEventStreams(t *testing.T) {
	e := usagetest.NewEnv(t)
	srv := httptest.NewServer(NewRouter(NewHandler(Services{Usage: e.Service, LiveCalls: e.Service}), WithToken("s3cret")))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/projects/" + e.ProjectA + "/usage/live")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", resp.StatusCode)
	}
	r := openLive(t, srv, e.ProjectA, http.Header{"Authorization": {"Bearer s3cret"}})
	r.cancel()
}

func TestTheLiveStreamAnswers503WithoutTheUsageService(t *testing.T) {
	srv := httptest.NewServer(NewRouter(NewHandler(Services{})))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/api/projects/p/usage/live")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestADocumentAssistantCallIsFilteredToTheProjectNamedInItsAttribution(t *testing.T) {
	srv, e := liveServer(t)
	r := openLive(t, srv, e.ProjectA, nil)
	ctx := context.Background()
	e.Service.Started(ctx, startReq("other", "harness:document-assistant:"+e.ProjectB, `{}`))
	e.Service.Started(ctx, startReq("mine", "harness:document-assistant:"+e.ProjectA, `{}`))

	ev := r.next()
	if ev.Data["id"] != "mine" || ev.Data["kind"] != "document-assistant" || ev.Data["project_id"] != e.ProjectA {
		t.Fatalf("document assistant call = %+v", ev.Data)
	}
	r.nothingMore()
}
