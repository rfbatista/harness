package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// hubOrch is an orchestration whose session events go through the real hub,
// stored first and then published, as the service records them.
type hubOrch struct {
	ports.Orchestration
	hub *orchestration.Hub
	mu  sync.Mutex
	log []ports.SessionEvent
}

func newHubOrch() *hubOrch {
	// A small replay ring, so resuming has to read the durable log.
	return &hubOrch{hub: orchestration.NewHub(16)}
}

func (o *hubOrch) Subscribe(id string) (<-chan ports.SessionEvent, []ports.SessionEvent, func()) {
	return o.hub.Subscribe(id)
}

func (o *hubOrch) History(_ string, fromSeq int64) ([]ports.SessionEvent, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []ports.SessionEvent
	for _, ev := range o.log {
		if ev.Seq > fromSeq {
			out = append(out, ev)
		}
	}
	return out, nil
}

// publishMany records n events on s1, each big enough that a follower which
// does not read falls behind, and fails if publishing ever waits for it.
func (o *hubOrch) publishMany(t *testing.T, n int) {
	t.Helper()
	text := strings.Repeat("x", 4096)
	start := time.Now()
	for i := 1; i <= n; i++ {
		ev := ports.SessionEvent{Seq: int64(i), SessionID: "s1", Type: "output", Text: text}
		o.mu.Lock()
		o.log = append(o.log, ev)
		o.mu.Unlock()
		o.hub.Publish("s1", ev)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("publishing %d events took %s: the publisher waited for a follower", n, d)
	}
}

// contiguous fails unless seqs run from first up by one.
func contiguous(t *testing.T, seqs []int64, first int64) {
	t.Helper()
	for i, s := range seqs {
		if s != first+int64(i) {
			t.Fatalf("seq %d at position %d, want %d: a gap", s, i, first+int64(i))
		}
	}
}

const manyEvents = 4000

// A session-feed follower on the page stream that does not read gets end for
// that path, after an unbroken run of events. Following again from the last
// seq it holds brings the rest.
func TestStream_ASlowSessionFollowerGetsEndAndNeverAGap(t *testing.T) {
	orch := newHubOrch()
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: orch}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	s := openStream(t, srv)

	const path = "/sessions/s1/events?from_seq=0"
	if code, body := s.post("follow", path); code != http.StatusOK {
		t.Fatalf("follow = %d %v", code, body)
	}
	orch.publishMany(t, manyEvents)

	var seqs []int64
	for ended := false; !ended; {
		m := s.next()
		if m["path"] != path {
			t.Fatalf("message = %v, want one of %s", m, path)
		}
		if m["end"] == true {
			ended = true
			continue
		}
		seqs = append(seqs, int64(m["data"].(map[string]any)["seq"].(float64)))
	}
	if len(seqs) == manyEvents {
		t.Fatal("the follower kept up; the test never made it fall behind")
	}
	contiguous(t, seqs, 1)

	last := int64(len(seqs))
	again := "/sessions/s1/events?from_seq=" + strconv.FormatInt(last, 10)
	if code, body := s.post("follow", again); code != http.StatusOK {
		t.Fatalf("follow again = %d %v", code, body)
	}
	var rest []int64
	for int64(len(rest)) < manyEvents-last {
		m := s.next()
		if m["path"] != again || m["end"] == true {
			t.Fatalf("message = %v, want the rest of %s", m, again)
		}
		rest = append(rest, int64(m["data"].(map[string]any)["seq"].(float64)))
	}
	contiguous(t, rest, last+1)
}

// openSessionSSE opens GET /api/sessions/s1/events, resuming after
// lastEventID when it is set.
func openSessionSSE(t *testing.T, srv *httptest.Server, lastEventID string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/sessions/s1/events", nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// readIDs reads event ids from an SSE body until it closes or until wants
// ids have arrived (wants 0: until it closes).
func readIDs(t *testing.T, resp *http.Response, wants int) []int64 {
	t.Helper()
	ids := make(chan int64, manyEvents)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scan := bufio.NewScanner(resp.Body)
		scan.Buffer(make([]byte, 64*1024), 1024*1024)
		for scan.Scan() {
			if v, ok := strings.CutPrefix(scan.Text(), "id: "); ok {
				n, _ := strconv.ParseInt(v, 10, 64)
				ids <- n
			}
		}
	}()
	var got []int64
	timeout := time.After(10 * time.Second)
	for {
		select {
		case id := <-ids:
			got = append(got, id)
			if wants > 0 && len(got) == wants {
				return got
			}
		case <-done:
			for {
				select {
				case id := <-ids:
					got = append(got, id)
				default:
					if wants > 0 {
						t.Fatalf("the connection closed after %d of %d events", len(got), wants)
					}
					return got
				}
			}
		case <-timeout:
			t.Fatalf("after %d events the connection neither closed nor sent more", len(got))
		}
	}
}

// The per-feed session SSE endpoint closes a slow follower's connection after
// an unbroken run of events; reconnecting with Last-Event-ID resumes without
// loss.
func TestSessionEvents_ASlowFollowerIsClosedAndResumesWithoutLoss(t *testing.T) {
	orch := newHubOrch()
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: orch}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })

	resp := openSessionSSE(t, srv, "")
	orch.publishMany(t, manyEvents)

	ids := readIDs(t, resp, 0)
	if len(ids) == manyEvents {
		t.Fatal("the follower kept up; the test never made it fall behind")
	}
	contiguous(t, ids, 1)

	last := int64(len(ids))
	rest := readIDs(t, openSessionSSE(t, srv, strconv.FormatInt(last, 10)), int(manyEvents-last))
	contiguous(t, rest, last+1)
}

// overlapOrch answers a follow whose stored backlog and live events overlap,
// as they do when an event is stored while the follower reads the log.
type overlapOrch struct {
	ports.Orchestration
	live chan ports.SessionEvent
}

func (o *overlapOrch) Subscribe(string) (<-chan ports.SessionEvent, []ports.SessionEvent, func()) {
	return o.live, nil, func() {}
}

func (o *overlapOrch) History(string, int64) ([]ports.SessionEvent, error) {
	return []ports.SessionEvent{{Seq: 6, Type: "a"}, {Seq: 7, Type: "b"}}, nil
}

// Events read from the log are not sent again when they also arrive live.
func TestFollowSession_ResumesFromTheLogWithoutDuplicates(t *testing.T) {
	orch := &overlapOrch{live: make(chan ports.SessionEvent, 4)}
	orch.live <- ports.SessionEvent{Seq: 7, Type: "b"}
	orch.live <- ports.SessionEvent{Type: "output_delta"}
	orch.live <- ports.SessionEvent{Seq: 8, Type: "c"}
	h := &Handler{orchSvc: orch}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out, err := h.followSession(ctx, "s1", 5)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(got) < 4 {
		select {
		case v := <-out:
			ev := v.(ports.SessionEvent)
			got = append(got, ev.Type+strconv.FormatInt(ev.Seq, 10))
		case <-time.After(5 * time.Second):
			t.Fatalf("got %v, then nothing", got)
		}
	}
	if strings.Join(got, ",") != "a6,b7,output_delta0,c8" {
		t.Fatalf("events = %v, want a6,b7,output_delta0,c8", got)
	}
}

// laggingLogOrch answers a follow whose replay does not reach back to the
// resume point while the log has nothing after it yet, and whose replay holds
// two events in the order they were published, which concurrent recordings
// may swap.
type laggingLogOrch struct {
	ports.Orchestration
	live chan ports.SessionEvent
}

func (o *laggingLogOrch) Subscribe(string) (<-chan ports.SessionEvent, []ports.SessionEvent, func()) {
	return o.live, []ports.SessionEvent{{Seq: 7, Type: "b"}, {Seq: 5, Type: "a"}}, func() {}
}

func (o *laggingLogOrch) History(string, int64) ([]ports.SessionEvent, error) {
	return nil, nil
}

// A follower resuming after seq 5 never gets seq 5 or an earlier one again,
// whatever the replay holds, when the log has nothing to add.
func TestFollowSession_NeverResendsWhatTheFollowerHad(t *testing.T) {
	orch := &laggingLogOrch{live: make(chan ports.SessionEvent, 4)}
	orch.live <- ports.SessionEvent{Seq: 8, Type: "c"}
	h := &Handler{orchSvc: orch}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out, err := h.followSession(ctx, "s1", 5)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(got) < 2 {
		select {
		case v := <-out:
			ev := v.(ports.SessionEvent)
			got = append(got, ev.Type+strconv.FormatInt(ev.Seq, 10))
		case <-time.After(5 * time.Second):
			t.Fatalf("got %v, then nothing", got)
		}
	}
	if strings.Join(got, ",") != "b7,c8" {
		t.Fatalf("events = %v, want b7,c8", got)
	}
}
