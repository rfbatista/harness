package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The SSE contract: a message carries either session or ticket, told apart by
// which key is present, so the key that does not apply must be absent, not null.
func TestProjectChange_WireShape(t *testing.T) {
	raw, _ := json.Marshal(ports.ProjectChange{Ticket: &domain.Ticket{ID: "tk1", ProjectID: "p1", Status: domain.TicketStatusDone}})
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, has := m["session"]; has {
		t.Fatalf("ticket message carries a session key: %s", raw)
	}
	if tk, ok := m["ticket"].(map[string]any); !ok || tk["id"] != "tk1" || tk["status"] != "done" {
		t.Fatalf("ticket message = %s", raw)
	}

	raw, _ = json.Marshal(ports.ProjectChange{Session: &domain.Session{ID: "s1", ProjectID: "p1"}})
	m = nil
	_ = json.Unmarshal(raw, &m)
	if _, has := m["ticket"]; has {
		t.Fatalf("session message carries a ticket key: %s", raw)
	}
	if s, ok := m["session"].(map[string]any); !ok || s["id"] != "s1" {
		t.Fatalf("session message = %s", raw)
	}

	raw, _ = json.Marshal(ports.ProjectChange{Ticket: &domain.Ticket{ID: "tk1", ProjectID: "p1"}, Deleted: true})
	if !strings.Contains(string(raw), `"deleted":true`) {
		t.Fatalf("deleted flag lost: %s", raw)
	}
}

// feedOnly is an orchestration that can only be followed; the events handler
// needs nothing else from it.
type feedOnly struct {
	ports.Orchestration
	ch chan ports.ProjectChange
}

func (f feedOnly) FollowProject(context.Context, string) (<-chan ports.SessionChange, error) {
	return f.ch, nil
}

// GET /api/events writes each change as one data: line, ticket changes included.
func TestHTTP_Events_StreamsTicketChanges(t *testing.T) {
	ch := make(chan ports.ProjectChange, 2)
	h := &Handler{orchSvc: feedOnly{ch: ch}}
	ch <- ports.ProjectChange{Ticket: &domain.Ticket{ID: "tk1", ProjectID: "p1", Title: "Ship it", Status: domain.TicketStatusReview}}
	ch <- ports.ProjectChange{Session: &domain.Session{ID: "s1", ProjectID: "p1"}}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events?project_id=p1", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		NewRouter(h).ServeHTTP(rec, req)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && strings.Count(rec.Body.String(), "data: ") < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	lines := []string{}
	for _, l := range strings.Split(rec.Body.String(), "\n") {
		if data, ok := strings.CutPrefix(l, "data: "); ok {
			lines = append(lines, data)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("data lines = %v", lines)
	}
	var first, second map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if tk, ok := first["ticket"].(map[string]any); !ok || tk["status"] != "review" || first["session"] != nil {
		t.Fatalf("first = %s", lines[0])
	}
	if s, ok := second["session"].(map[string]any); !ok || s["id"] != "s1" || second["ticket"] != nil {
		t.Fatalf("second = %s", lines[1])
	}
}
