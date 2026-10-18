package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// streamOrch is an orchestration whose project feeds and session events the
// test drives; it records each follow and when its follower stopped.
type streamOrch struct {
	ports.Orchestration
	mu      sync.Mutex
	feeds   map[string]chan ports.ProjectChange
	follows map[string]int
	stopped map[string]chan struct{}
	replay  []ports.SessionEvent
	live    chan ports.SessionEvent
}

func newStreamOrch() *streamOrch {
	return &streamOrch{
		feeds:   map[string]chan ports.ProjectChange{},
		follows: map[string]int{},
		stopped: map[string]chan struct{}{},
		live:    make(chan ports.SessionEvent, 4),
	}
}

func (o *streamOrch) FollowProject(ctx context.Context, projectID string) (<-chan ports.SessionChange, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	ch := make(chan ports.ProjectChange, 4)
	stopped := make(chan struct{})
	o.feeds[projectID], o.stopped[projectID] = ch, stopped
	o.follows[projectID]++
	go func() { <-ctx.Done(); close(stopped) }()
	return ch, nil
}

func (o *streamOrch) Subscribe(string) (<-chan ports.SessionEvent, []ports.SessionEvent, func()) {
	return o.live, o.replay, func() {}
}

func (o *streamOrch) feed(project string) chan ports.ProjectChange {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.feeds[project]
}

func (o *streamOrch) followed(project string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.follows[project]
}

func (o *streamOrch) waitStopped(t *testing.T, project string) {
	t.Helper()
	o.mu.Lock()
	stopped := o.stopped[project]
	o.mu.Unlock()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatalf("the feed of %s was never stopped", project)
	}
}

// tabStream is one open GET /api/stream, read message by message.
type tabStream struct {
	t      *testing.T
	srv    *httptest.Server
	id     string
	cancel context.CancelFunc
	lines  chan string
}

func openStream(t *testing.T, srv *httptest.Server) *tabStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	s := &tabStream{t: t, srv: srv, cancel: cancel, lines: make(chan string, 16)}
	go func() {
		defer resp.Body.Close()
		scan := bufio.NewScanner(resp.Body)
		for scan.Scan() {
			if data, ok := strings.CutPrefix(scan.Text(), "data: "); ok {
				s.lines <- data
			}
		}
		close(s.lines)
	}()
	hello := s.next()
	s.id, _ = hello["stream"].(string)
	if s.id == "" {
		t.Fatalf("first message = %v, want the stream id", hello)
	}
	return s
}

func (s *tabStream) next() map[string]any {
	s.t.Helper()
	select {
	case line, ok := <-s.lines:
		if !ok {
			s.t.Fatal("the stream ended")
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			s.t.Fatalf("message %q: %v", line, err)
		}
		return m
	case <-time.After(5 * time.Second):
		s.t.Fatal("no message")
		return nil
	}
}

func (s *tabStream) post(route, path string) (int, map[string]any) {
	s.t.Helper()
	return postStream(s.t, s.srv, route, s.id, path)
}

func postStream(t *testing.T, srv *httptest.Server, route, stream, path string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"stream": stream, "path": path})
	resp, err := http.Post(srv.URL+"/api/stream/"+route, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func streamServer(t *testing.T, orch *streamOrch) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: orch}))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}

// One connection carries several feeds, each change tagged with the path of
// the feed it came from.
func TestStream_CarriesSeveralFeedsTaggedByPath(t *testing.T) {
	orch := newStreamOrch()
	srv := streamServer(t, orch)
	s := openStream(t, srv)

	for _, p := range []string{"/events?project_id=p1", "/events?project_id=p2"} {
		if code, body := s.post("follow", p); code != http.StatusOK {
			t.Fatalf("follow %s = %d %v", p, code, body)
		}
	}
	orch.feed("p2") <- ports.ProjectChange{Ticket: &domain.Ticket{ID: "t2", ProjectID: "p2"}}
	m := s.next()
	if m["path"] != "/events?project_id=p2" || m["data"].(map[string]any)["ticket"].(map[string]any)["id"] != "t2" {
		t.Fatalf("message = %v", m)
	}
	orch.feed("p1") <- ports.ProjectChange{Session: &domain.Session{ID: "s1", ProjectID: "p1"}}
	m = s.next()
	if m["path"] != "/events?project_id=p1" || m["data"].(map[string]any)["session"].(map[string]any)["id"] != "s1" {
		t.Fatalf("message = %v", m)
	}
}

// Following a path twice is one follow; unfollowing it stops the feed.
func TestStream_FollowIsIdempotentAndUnfollowStopsTheFeed(t *testing.T) {
	orch := newStreamOrch()
	srv := streamServer(t, orch)
	s := openStream(t, srv)

	s.post("follow", "/events?project_id=p1")
	s.post("follow", "/events?project_id=p1")
	if n := orch.followed("p1"); n != 1 {
		t.Fatalf("followed %d times, want 1", n)
	}
	if code, _ := s.post("unfollow", "/events?project_id=p1"); code != http.StatusOK {
		t.Fatalf("unfollow = %d", code)
	}
	orch.waitStopped(t, "p1")
	if code, _ := s.post("unfollow", "/events?project_id=p1"); code != http.StatusOK {
		t.Fatalf("unfollowing what is not followed = %d, want 200", code)
	}
}

// A feed that ends on its own (its follower fell behind) says so, and can be
// followed again.
func TestStream_AFeedThatEndsSaysSo(t *testing.T) {
	orch := newStreamOrch()
	srv := streamServer(t, orch)
	s := openStream(t, srv)

	s.post("follow", "/events?project_id=p1")
	close(orch.feed("p1"))
	if m := s.next(); m["path"] != "/events?project_id=p1" || m["end"] != true {
		t.Fatalf("message = %v, want the end of p1", m)
	}
	s.post("follow", "/events?project_id=p1")
	if n := orch.followed("p1"); n != 2 {
		t.Fatalf("followed %d times, want 2", n)
	}
}

// A session's events come buffered ones first, then live ones.
func TestStream_SessionEventsReplayThenLive(t *testing.T) {
	orch := newStreamOrch()
	orch.replay = []ports.SessionEvent{{Seq: 1, Type: "a"}, {Seq: 2, Type: "b"}}
	srv := streamServer(t, orch)
	s := openStream(t, srv)

	s.post("follow", "/sessions/s1/events?from_seq=1")
	orch.live <- ports.SessionEvent{Seq: 3, Type: "c"}
	for _, want := range []float64{2, 3} {
		m := s.next()
		if m["path"] != "/sessions/s1/events?from_seq=1" || m["data"].(map[string]any)["seq"] != want {
			t.Fatalf("message = %v, want seq %v", m, want)
		}
	}
}

func TestStream_Errors(t *testing.T) {
	orch := newStreamOrch()
	srv := streamServer(t, orch)
	s := openStream(t, srv)

	if code, body := postStream(t, srv, "follow", "nope", "/events?project_id=p1"); code != http.StatusNotFound || body["code"] != "STREAM_NOT_FOUND" {
		t.Fatalf("unknown stream = %d %v", code, body)
	}
	for _, p := range []string{"/list_projects", "/sessions//events", "/projects/p1/usage", "::"} {
		if code, body := s.post("follow", p); code != http.StatusBadRequest || body["code"] != "INVALID_INPUT" {
			t.Fatalf("follow %q = %d %v", p, code, body)
		}
	}
	if code, _ := s.post("follow", "/notifications/events"); code != http.StatusServiceUnavailable {
		t.Fatalf("an unconfigured feed = %d, want 503", code)
	}
}

// Closing the connection stops every feed it followed and forgets the stream.
func TestStream_ClosingStopsItsFeeds(t *testing.T) {
	orch := newStreamOrch()
	srv := streamServer(t, orch)
	s := openStream(t, srv)

	s.post("follow", "/events?project_id=p1")
	s.cancel()
	orch.waitStopped(t, "p1")
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		code, _ := postStream(t, srv, "follow", s.id, "/events?project_id=p2")
		if code == http.StatusNotFound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("follow on a closed stream = %d, want 404", code)
		}
	}
}
