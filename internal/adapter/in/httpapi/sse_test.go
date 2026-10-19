package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// stuckFeedServer serves the API with a short write timeout, so a client that
// stops reading is found out quickly.
func stuckFeedServer(t *testing.T, orch ports.Orchestration) *httptest.Server {
	t.Helper()
	h := &Handler{orchSvc: orch}
	h.streams.writeTimeout = 200 * time.Millisecond
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}

// openUnread opens the SSE endpoint at path and never reads its body.
func openUnread(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", path, resp.StatusCode)
	}
	return resp
}

// closesSoon fails unless the server has closed resp's connection: reading
// what it buffered comes to an end.
func closesSoon(t *testing.T, resp *http.Response) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection was never closed")
	}
}

// A client of a project's feed that stops reading has its connection closed
// once a write cannot go through in time, and the feed is stopped.
func TestEvents_AStuckClientIsClosedAndItsFeedStopped(t *testing.T) {
	orch := newStreamOrch()
	srv := stuckFeedServer(t, orch)
	resp := openUnread(t, srv, "/api/events?project_id=p1")

	orch.mu.Lock()
	feed, stopped := orch.feeds["p1"], orch.stopped["p1"]
	orch.mu.Unlock()
	big := ports.ProjectChange{Ticket: &domain.Ticket{ID: "big", ProjectID: "p1", Description: strings.Repeat("x", 32<<10)}}
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-stopped:
		case <-deadline:
			t.Fatal("the stuck client's feed was never stopped")
		case <-tick.C:
			select {
			case feed <- big:
			case <-stopped:
			case <-deadline:
				t.Fatal("the stuck client's feed was never stopped")
			}
			continue
		}
		break
	}
	closesSoon(t, resp)
}

// unsubOrch is a hub-backed orchestration that says when a follower
// unsubscribed.
type unsubOrch struct {
	*hubOrch
	unsubscribed chan struct{}
}

func (o *unsubOrch) Subscribe(id string) (<-chan ports.SessionEvent, []ports.SessionEvent, func()) {
	ch, replay, cancel := o.hubOrch.Subscribe(id)
	return ch, replay, func() {
		cancel()
		close(o.unsubscribed)
	}
}

// A client of a session's events that stops reading has its connection
// closed once a write cannot go through in time, and its subscription is
// cancelled.
func TestSessionEvents_AStuckClientIsClosedAndUnsubscribed(t *testing.T) {
	orch := &unsubOrch{hubOrch: newHubOrch(), unsubscribed: make(chan struct{})}
	srv := stuckFeedServer(t, orch)
	resp := openUnread(t, srv, "/api/sessions/s1/events")

	text := strings.Repeat("x", 64<<10)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for seq, deadline := int64(1), time.After(10*time.Second); ; seq++ {
		select {
		case <-orch.unsubscribed:
		case <-deadline:
			t.Fatal("the stuck client was never unsubscribed")
		case <-tick.C:
			orch.hub.Publish("s1", ports.SessionEvent{Seq: seq, SessionID: "s1", Type: "output", Text: text})
			continue
		}
		break
	}
	closesSoon(t, resp)
}
