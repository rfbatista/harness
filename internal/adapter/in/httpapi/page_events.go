package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// The page event stream: the one SSE connection a page holds, carrying the
// project's and the inbox's changes in envelopes told apart by kind (see
// "Contract: Harness server ↔ Web UI — page event stream").

// pageEventsPing is how often the stream sends a `: ping` comment; tests
// shorten it.
var pageEventsPing = 15 * time.Second

type helloEnvelope struct {
	Kind     string `json:"kind"`
	StreamID string `json:"stream_id"`
}

// projectEnvelope carries what /api/events sends, under change.
type projectEnvelope struct {
	Kind   string              `json:"kind"`
	Change ports.ProjectChange `json:"change"`
}

// notificationsEnvelope carries the keys /api/notifications/events sends,
// beside kind.
type notificationsEnvelope struct {
	Kind string `json:"kind"`
	ports.NotificationChange
}

// pageStream is one open page event stream. Only the handler's goroutine
// writes its response; other calls reach the stream through the registry by
// id.
type pageStream struct {
	id string
}

// pageStreamRegistry holds the open page event streams by stream_id. The
// zero value is ready to use.
type pageStreamRegistry struct {
	mu      sync.Mutex
	streams map[string]*pageStream
}

// open registers a new stream under a random, unguessable id.
func (r *pageStreamRegistry) open() *pageStream {
	var b [16]byte
	_, _ = rand.Read(b[:])
	s := &pageStream{id: hex.EncodeToString(b[:])}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streams == nil {
		r.streams = map[string]*pageStream{}
	}
	r.streams[s.id] = s
	return s
}

func (r *pageStreamRegistry) lookup(id string) (*pageStream, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.streams[id]
	return s, ok
}

func (r *pageStreamRegistry) remove(s *pageStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, s.id)
}

// handlePageEvents streams the project's changes (none when project_id is
// empty) and the inbox's, each as one `data:` line of its envelope, after a
// hello naming the stream; a part whose service is not configured is left
// out. It reads the same followers /api/events and
// /api/notifications/events read, so it never holds back a send; when either
// falls behind the whole stream ends and the client reconnects.
func (h *Handler) handlePageEvents(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	// Ending the stream for a follower that fell behind releases the other.
	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()
	// A part whose service this server lacks stays nil, and nil never
	// delivers: the page keeps the parts it has.
	var (
		inbox   <-chan ports.NotificationChange
		project <-chan ports.ProjectChange
		err     error
	)
	if h.notifications != nil {
		if inbox, err = h.notifications.FollowNotifications(ctx); err != nil {
			return err
		}
	}
	if projectID != "" && h.orchSvc != nil {
		if project, err = h.orchSvc.FollowProject(ctx, projectID); err != nil {
			return err
		}
	}

	s := h.pageStreams.open()
	defer h.pageStreams.remove(s)
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writeEnvelope(w, helloEnvelope{Kind: "hello", StreamID: s.id})
	s.run(ctx, w, project, inbox)
	return nil
}

// run writes the stream's changes until ctx ends or a follower closes.
func (s *pageStream) run(ctx context.Context, w *echo.Response, project <-chan ports.ProjectChange, inbox <-chan ports.NotificationChange) {
	ticker := time.NewTicker(pageEventsPing)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case change, ok := <-project:
			if !ok {
				return
			}
			writeEnvelope(w, projectEnvelope{Kind: "project", Change: change})
		case change, ok := <-inbox:
			if !ok {
				return
			}
			writeEnvelope(w, notificationsEnvelope{Kind: "notifications", NotificationChange: change})
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}

func writeEnvelope(w *echo.Response, envelope any) {
	data, _ := json.Marshal(envelope)
	fmt.Fprintf(w, "data: %s\n\n", data)
	w.Flush()
}
