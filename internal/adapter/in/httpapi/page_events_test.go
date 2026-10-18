package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// followedProject is an orchestration whose project feed is a channel the
// test drives; it records which projects were followed and when each
// follower's ctx ends.
type followedProject struct {
	ports.Orchestration
	ch       chan ports.ProjectChange
	followed chan string
	released chan struct{}
}

func newFollowedProject() *followedProject {
	return &followedProject{ch: make(chan ports.ProjectChange, 4), followed: make(chan string, 4), released: make(chan struct{}, 4)}
}

func (f *followedProject) FollowProject(ctx context.Context, projectID string) (<-chan ports.SessionChange, error) {
	f.followed <- projectID
	go func() { <-ctx.Done(); f.released <- struct{}{} }()
	return f.ch, nil
}

// followedInbox is an inbox whose feed is a channel the test drives.
type followedInbox struct {
	ports.Notifications
	ch       chan ports.NotificationChange
	released chan struct{}
}

func newFollowedInbox() *followedInbox {
	return &followedInbox{ch: make(chan ports.NotificationChange, 4), released: make(chan struct{}, 4)}
}

func (f *followedInbox) FollowNotifications(ctx context.Context) (<-chan ports.NotificationChange, error) {
	go func() { <-ctx.Done(); f.released <- struct{}{} }()
	return f.ch, nil
}

// sseReader reads one SSE stream message by message.
type sseReader struct {
	t *testing.T
	r *bufio.Reader
}

func openSSE(t *testing.T, ctx context.Context, url string) (*sseReader, *http.Response) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("GET %s content-type = %q", url, ct)
	}
	return &sseReader{t: t, r: bufio.NewReader(resp.Body)}, resp
}

// next returns the next message's lines (data lines and comments), or io.EOF
// when the stream ended.
func (s *sseReader) next() ([]string, error) {
	var lines []string
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return lines, err
		}
		line = strings.TrimRight(line, "\n")
		if line == "" {
			if len(lines) > 0 {
				return lines, nil
			}
			continue
		}
		lines = append(lines, line)
	}
}

// data returns the next message's data payload, skipping comments.
func (s *sseReader) data() string {
	s.t.Helper()
	for {
		lines, err := s.next()
		if err != nil {
			s.t.Fatalf("stream ended after %q: %v", lines, err)
		}
		if len(lines) == 1 && strings.HasPrefix(lines[0], ":") {
			continue
		}
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "data: ") {
			s.t.Fatalf("message = %q, want exactly one data: line", lines)
		}
		return strings.TrimPrefix(lines[0], "data: ")
	}
}

func (s *sseReader) hello() string {
	s.t.Helper()
	raw := s.data()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		s.t.Fatal(err)
	}
	id, _ := m["stream_id"].(string)
	if m["kind"] != "hello" || len(id) < 16 || len(m) != 2 {
		s.t.Fatalf("first message = %s, want {kind: hello, stream_id}", raw)
	}
	return id
}

func (s *sseReader) ended() {
	s.t.Helper()
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := s.next(); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != io.EOF {
			s.t.Fatalf("stream ended with %v, want EOF", err)
		}
	case <-time.After(5 * time.Second):
		s.t.Fatal("stream did not end")
	}
}

func waitFor(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not happen", what)
	}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// oldPayload is what an old stream at path sends for the one change its feed
// carries.
func oldPayload(t *testing.T, h *Handler, path string) string {
	t.Helper()
	srv := httptest.NewServer(NewRouter(h))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+path)
	return s.data()
}

func TestThePageEventStreamCarriesProjectAndInboxChangesInTheirEnvelopes(t *testing.T) {
	project := ports.ProjectChange{Ticket: &domain.Ticket{ID: "tk1", ProjectID: "p1", Title: "Ship it", Status: domain.TicketStatusReview}}
	inbox := ports.NotificationChange{Changed: []*domain.Notification{{ID: "n1", ProjectID: "p1", Title: "hi"}}, UnreadCount: 3}

	oldProject, oldInbox := newFollowedProject(), newFollowedInbox()
	oldProject.ch <- project
	oldInbox.ch <- inbox
	old := &Handler{orchSvc: oldProject, notifications: oldInbox}
	wantProject := oldPayload(t, old, "/api/events?project_id=p1")
	wantInbox := oldPayload(t, old, "/api/notifications/events")

	feed, notes := newFollowedProject(), newFollowedInbox()
	h := &Handler{orchSvc: feed, notifications: notes}
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1")
	id := s.hello()
	if got := <-feed.followed; got != "p1" {
		t.Fatalf("followed project %q, want p1", got)
	}
	if _, ok := h.pageStreams.lookup(id); !ok {
		t.Fatalf("stream %s is not in the registry", id)
	}

	feed.ch <- project
	var env map[string]json.RawMessage
	raw := s.data()
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if string(env["kind"]) != `"project"` || string(env["change"]) != wantProject || len(env) != 2 {
		t.Fatalf("project envelope = %s, want kind project and change %s", raw, wantProject)
	}

	notes.ch <- inbox
	raw = s.data()
	env = nil
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	var oldEnv map[string]json.RawMessage
	_ = json.Unmarshal([]byte(wantInbox), &oldEnv)
	if string(env["kind"]) != `"notifications"` || len(env) != len(oldEnv)+1 {
		t.Fatalf("notifications envelope = %s, want kind notifications and %s", raw, wantInbox)
	}
	for k, v := range oldEnv {
		if string(env[k]) != string(v) {
			t.Fatalf("notifications envelope %s = %s, want %s", k, env[k], v)
		}
	}

	other, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1")
	if otherID := other.hello(); otherID == id {
		t.Fatalf("two open streams share stream_id %s", id)
	}
}

func TestAPageEventStreamOutsideAProjectCarriesOnlyTheInbox(t *testing.T) {
	feed, notes := newFollowedProject(), newFollowedInbox()
	h := &Handler{orchSvc: feed, notifications: notes}
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events")
	s.hello()

	notes.ch <- ports.NotificationChange{Changed: []*domain.Notification{}, UnreadCount: 1}
	if raw := s.data(); !strings.Contains(raw, `"kind":"notifications"`) {
		t.Fatalf("message = %s, want an inbox change", raw)
	}
	select {
	case p := <-feed.followed:
		t.Fatalf("followed project %q with no project_id", p)
	default:
	}
}

func TestAPageEventStreamEndsWhenEitherPartFallsBehind(t *testing.T) {
	for _, part := range []string{"project", "notifications"} {
		t.Run(part, func(t *testing.T) {
			feed, notes := newFollowedProject(), newFollowedInbox()
			h := &Handler{orchSvc: feed, notifications: notes}
			srv := httptest.NewServer(NewRouter(h))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1")
			id := s.hello()

			if part == "project" {
				close(feed.ch)
			} else {
				close(notes.ch)
			}
			s.ended()
			waitFor(t, "project follower released", feed.released)
			waitFor(t, "inbox follower released", notes.released)
			waitUntil(t, "stream leaving the registry", func() bool { _, ok := h.pageStreams.lookup(id); return !ok })
		})
	}
}

func TestAPageEventStreamReleasesItsFollowersWhenTheClientGoes(t *testing.T) {
	feed, notes := newFollowedProject(), newFollowedInbox()
	h := &Handler{orchSvc: feed, notifications: notes}
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1")
	id := s.hello()

	cancel()
	waitFor(t, "project follower released", feed.released)
	waitFor(t, "inbox follower released", notes.released)
	waitUntil(t, "stream leaving the registry", func() bool { _, ok := h.pageStreams.lookup(id); return !ok })
}

func TestAPageEventStreamPings(t *testing.T) {
	defer func(d time.Duration) { pageEventsPing = d }(pageEventsPing)
	pageEventsPing = 20 * time.Millisecond

	h := &Handler{orchSvc: newFollowedProject(), notifications: newFollowedInbox()}
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1")
	s.hello()
	lines, err := s.next()
	if err != nil || len(lines) != 1 || lines[0] != ": ping" {
		t.Fatalf("after hello = %q (%v), want a : ping comment", lines, err)
	}
}

// A server built without the inbox (or without orchestration) still gives a
// page its stream, with the parts it has: a page must not lose its project
// changes because the inbox is off.
func TestAPageEventStreamWithoutTheInboxCarriesTheProject(t *testing.T) {
	feed := newFollowedProject()
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: feed}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1")
	s.hello()

	feed.ch <- ports.ProjectChange{Ticket: &domain.Ticket{ID: "t1"}}
	if raw := s.data(); !strings.Contains(raw, `"kind":"project"`) {
		t.Fatalf("message = %s, want a project change", raw)
	}
}

func TestAPageEventStreamWithoutOrchestrationCarriesTheInbox(t *testing.T) {
	notes := newFollowedInbox()
	srv := httptest.NewServer(NewRouter(&Handler{notifications: notes}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1")
	s.hello()

	notes.ch <- ports.NotificationChange{Changed: []*domain.Notification{}, UnreadCount: 2}
	if raw := s.data(); !strings.Contains(raw, `"kind":"notifications"`) {
		t.Fatalf("message = %s, want an inbox change", raw)
	}
}
