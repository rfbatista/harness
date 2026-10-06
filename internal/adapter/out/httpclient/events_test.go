package httpclient_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/httpclient"
)

// The TUI follows sessions only. A ticket message on the shared feed is not a
// session change and must never reach it, let alone as a nil session.
func TestEvents_SkipsTicketMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"ticket\":{\"id\":\"tk1\",\"project_id\":\"p1\",\"status\":\"review\"}}\n\n")
		fmt.Fprint(w, ": ping\n\n")
		fmt.Fprint(w, "data: {\"session\":{\"id\":\"s1\",\"project_id\":\"p1\"}}\n\n")
		fmt.Fprint(w, "data: {\"ticket\":{\"id\":\"tk1\",\"project_id\":\"p1\"},\"deleted\":true}\n\n")
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	changes, err := httpclient.NewEvents(httpclient.New(srv.URL)).FollowProject(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for {
		select {
		case c, ok := <-changes:
			if !ok {
				if len(seen) != 1 || seen[0] != "s1" {
					t.Fatalf("sessions seen = %v, want only s1", seen)
				}
				return
			}
			if c.Session == nil {
				t.Fatalf("a change without a session reached the TUI: %+v", c)
			}
			seen = append(seen, c.Session.ID)
		case <-time.After(5 * time.Second):
			t.Fatal("stream did not end")
		}
	}
}
