package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// followedSessions is an orchestration whose sessions publish through the
// real hub, so replay and the slow-subscriber rule are the hub's own; it
// records each session subscription's release.
type followedSessions struct {
	*followedProject
	hub      *orchestration.Hub
	known    map[string]bool
	released chan string
}

func newFollowedSessions(ids ...string) *followedSessions {
	f := &followedSessions{followedProject: newFollowedProject(), hub: orchestration.NewHub(64), known: map[string]bool{}, released: make(chan string, 16)}
	for _, id := range ids {
		f.known[id] = true
	}
	return f
}

func (f *followedSessions) Get(_ context.Context, id string) (*domain.Session, error) {
	if !f.known[id] {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	return &domain.Session{ID: id}, nil
}

func (f *followedSessions) Subscribe(id string) (<-chan ports.SessionEvent, []ports.SessionEvent, func()) {
	ch, replay, cancel := f.hub.Subscribe(id)
	return ch, replay, func() { cancel(); f.released <- id }
}

// publish sends a persisted event the way a running session does.
func (f *followedSessions) publish(id string, seq int64, text string) {
	f.hub.Publish(id, ports.SessionEvent{Seq: seq, SessionID: id, Type: "output", Text: text})
}

type followEnv struct {
	t        *testing.T
	sessions *followedSessions
	notes    *followedInbox
	h        *Handler
	srv      *httptest.Server
}

func newFollowEnv(t *testing.T, ids ...string) *followEnv {
	sessions, notes := newFollowedSessions(ids...), newFollowedInbox()
	h := &Handler{orchSvc: sessions, notifications: notes}
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)
	return &followEnv{t: t, sessions: sessions, notes: notes, h: h, srv: srv}
}

// open opens a page event stream on project p1 and returns it with its id.
func (e *followEnv) open(ctx context.Context) (*sseReader, string) {
	e.t.Helper()
	s, _ := openSSE(e.t, ctx, e.srv.URL+"/api/page_events?project_id=p1")
	id := s.hello()
	<-e.sessions.followed
	return s, id
}

// follow posts /api/follow_session and returns its status and error code.
func (e *followEnv) follow(body string) (int, string) {
	e.t.Helper()
	resp, err := http.Post(e.srv.URL+"/api/follow_session", "application/json", strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Code
}

func (e *followEnv) mustFollow(streamID, sessionID string, afterSeq ...int64) {
	e.t.Helper()
	sid := "null"
	if sessionID != "" {
		sid = fmt.Sprintf("%q", sessionID)
	}
	body := fmt.Sprintf(`{"stream_id":%q,"session_id":%s`, streamID, sid)
	if len(afterSeq) > 0 {
		body += fmt.Sprintf(`,"after_seq":%d`, afterSeq[0])
	}
	body += "}"
	if status, code := e.follow(body); status != http.StatusNoContent {
		e.t.Fatalf("follow %s = %d %s, want 204", body, status, code)
	}
}

// sessionEvent reads the next message, which must be a session envelope for
// sessionID, and returns its event exactly as sent.
func sessionEvent(t *testing.T, s *sseReader, sessionID string) string {
	t.Helper()
	raw := s.data()
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if string(env["kind"]) != `"session"` || string(env["session_id"]) != fmt.Sprintf("%q", sessionID) || len(env) != 3 {
		t.Fatalf("message = %s, want a session envelope for %s", raw, sessionID)
	}
	return string(env["event"])
}

func seqOf(t *testing.T, event string) int64 {
	t.Helper()
	var ev ports.SessionEvent
	if err := json.Unmarshal([]byte(event), &ev); err != nil {
		t.Fatal(err)
	}
	return ev.Seq
}

// oldSessionPayloads reads n data payloads from /api/sessions/:id/events.
func (e *followEnv) oldSessionPayloads(id string, n int) []string {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(e.t, ctx, e.srv.URL+"/api/sessions/"+id+"/events")
	var out []string
	for len(out) < n {
		lines, err := s.next()
		if err != nil {
			e.t.Fatalf("old stream ended after %d payloads: %v", len(out), err)
		}
		for _, l := range lines {
			if strings.HasPrefix(l, "data: ") {
				out = append(out, strings.TrimPrefix(l, "data: "))
			}
		}
	}
	return out
}

func TestFollowingASessionCarriesItsReplayThenItsLiveEvents(t *testing.T) {
	e := newFollowEnv(t, "A")
	e.sessions.publish("A", 1, "first <b>&</b>")
	e.sessions.publish("A", 2, "second")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, id := e.open(ctx)

	e.mustFollow(id, "A")
	got := []string{sessionEvent(t, s, "A"), sessionEvent(t, s, "A")}
	// Published after the 204: the stream is already subscribed.
	e.sessions.publish("A", 3, "live")
	got = append(got, sessionEvent(t, s, "A"))

	want := e.oldSessionPayloads("A", 3)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %s, want what the old stream sends: %s", i, got[i], want[i])
		}
	}
}

func TestFollowingASessionAfterASeqReplaysOnlyLaterEvents(t *testing.T) {
	e := newFollowEnv(t, "A")
	for seq := int64(1); seq <= 3; seq++ {
		e.sessions.publish("A", seq, fmt.Sprint("event ", seq))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, id := e.open(ctx)

	e.mustFollow(id, "A", 2)
	if seq := seqOf(t, sessionEvent(t, s, "A")); seq != 3 {
		t.Fatalf("first replayed seq = %d, want 3", seq)
	}
	e.sessions.publish("A", 4, "live")
	if seq := seqOf(t, sessionEvent(t, s, "A")); seq != 4 {
		t.Fatalf("next seq = %d, want 4", seq)
	}
}

func TestFollowingAnotherSessionStopsThePreviousOne(t *testing.T) {
	e := newFollowEnv(t, "A", "B")
	e.sessions.publish("B", 1, "b1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, id := e.open(ctx)

	e.mustFollow(id, "A")
	e.sessions.publish("A", 1, "a1")
	sessionEvent(t, s, "A")

	e.mustFollow(id, "B")
	select {
	case got := <-e.sessions.released:
		if got != "A" {
			t.Fatalf("released %s, want A", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("A's subscription was not released")
	}
	e.sessions.publish("A", 2, "a2")
	e.sessions.publish("B", 2, "b2")
	if seq := seqOf(t, sessionEvent(t, s, "B")); seq != 1 {
		t.Fatalf("B replay seq = %d, want 1", seq)
	}
	if seq := seqOf(t, sessionEvent(t, s, "B")); seq != 2 {
		t.Fatalf("B live seq = %d, want 2", seq)
	}
	e.sessions.ch <- ports.ProjectChange{Ticket: &domain.Ticket{ID: "t1"}}
	if raw := s.data(); !strings.Contains(raw, `"kind":"project"`) {
		t.Fatalf("message = %s, want the project change and no more of A", raw)
	}
}

func TestUnfollowingStopsSessionEventsWhileProjectAndInboxContinue(t *testing.T) {
	e := newFollowEnv(t, "A")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, id := e.open(ctx)

	e.mustFollow(id, "A")
	e.mustFollow(id, "")
	select {
	case <-e.sessions.released:
	case <-time.After(5 * time.Second):
		t.Fatal("A's subscription was not released")
	}
	e.sessions.publish("A", 1, "after unfollow")
	e.sessions.ch <- ports.ProjectChange{Ticket: &domain.Ticket{ID: "t1"}}
	if raw := s.data(); !strings.Contains(raw, `"kind":"project"`) {
		t.Fatalf("message = %s, want the project change", raw)
	}
	e.notes.ch <- ports.NotificationChange{Changed: []*domain.Notification{}, UnreadCount: 1}
	if raw := s.data(); !strings.Contains(raw, `"kind":"notifications"`) {
		t.Fatalf("message = %s, want the inbox change", raw)
	}
}

func TestFollowingOnAnUnknownOrEndedStreamAnswersStreamNotFound(t *testing.T) {
	e := newFollowEnv(t, "A")
	if status, code := e.follow(`{"stream_id":"nope","session_id":"A"}`); status != http.StatusNotFound || code != "STREAM_NOT_FOUND" {
		t.Fatalf("unknown stream = %d %s, want 404 STREAM_NOT_FOUND", status, code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	_, id := e.open(ctx)
	cancel()
	waitUntil(t, "stream leaving the registry", func() bool { _, ok := e.h.pageStreams.lookup(id); return !ok })
	if status, code := e.follow(fmt.Sprintf(`{"stream_id":%q,"session_id":"A"}`, id)); status != http.StatusNotFound || code != "STREAM_NOT_FOUND" {
		t.Fatalf("ended stream = %d %s, want 404 STREAM_NOT_FOUND", status, code)
	}
}

// A follow that finds the stream just as it ends answers rather than waiting
// on a loop that is gone.
func TestFollowingAStreamThatIsEndingAnswersStreamNotFound(t *testing.T) {
	e := newFollowEnv(t, "A")
	ending := e.h.pageStreams.open()
	close(ending.done)
	if status, code := e.follow(fmt.Sprintf(`{"stream_id":%q,"session_id":"A"}`, ending.id)); status != http.StatusNotFound || code != "STREAM_NOT_FOUND" {
		t.Fatalf("ending stream = %d %s, want 404 STREAM_NOT_FOUND", status, code)
	}
}

func TestFollowingAnUnknownSessionAnswersSessionNotFound(t *testing.T) {
	e := newFollowEnv(t, "A")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, id := e.open(ctx)
	if status, code := e.follow(fmt.Sprintf(`{"stream_id":%q,"session_id":"nope"}`, id)); status != http.StatusNotFound || code != "SESSION_NOT_FOUND" {
		t.Fatalf("unknown session = %d %s, want 404 SESSION_NOT_FOUND", status, code)
	}
}

func TestEndingAPageEventStreamReleasesItsSessionSubscription(t *testing.T) {
	e := newFollowEnv(t, "A")
	ctx, cancel := context.WithCancel(context.Background())
	_, id := e.open(ctx)
	e.mustFollow(id, "A")

	cancel()
	select {
	case got := <-e.sessions.released:
		if got != "A" {
			t.Fatalf("released %s, want A", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session subscription outlived the stream")
	}
}

// A burst the stream cannot keep up with is the hub's to drop: publishing
// never waits on the page, and the stream stays open and carries what comes
// next.
func TestASlowSessionPartNeitherBlocksTheHubNorEndsTheStream(t *testing.T) {
	e := newFollowEnv(t, "A")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, id := e.open(ctx)
	e.mustFollow(id, "A")

	published := make(chan struct{})
	go func() {
		defer close(published)
		for seq := int64(1); seq <= 20000; seq++ {
			e.sessions.publish("A", seq, strings.Repeat("x", 512))
		}
	}()
	waitFor(t, "the burst publishing without waiting on the page", published)

	// What the hub kept arrives in order, and so do the events after the
	// burst: they keep coming until the drained stream has room for one.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for seq := int64(20001); ; seq++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				e.sessions.publish("A", seq, "after the burst")
			}
		}
	}()
	last := int64(0)
	for last <= 20000 {
		seq := seqOf(t, sessionEvent(t, s, "A"))
		if seq <= last {
			t.Fatalf("seq %d after %d, want increasing", seq, last)
		}
		last = seq
	}
}
