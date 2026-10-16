package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The page event stream: the one SSE connection a page holds, carrying the
// project's and the inbox's changes, and the events of the one session it
// follows, in envelopes told apart by kind (see "Contract: Harness server ↔
// Web UI — page event stream").

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

// usageEnvelope carries what /api/projects/:p/usage/events sends, under
// record.
type usageEnvelope struct {
	Kind   string              `json:"kind"`
	Record *domain.UsageRecord `json:"record"`
}

// projectsEnvelope carries what /api/project_events sends, under change.
type projectsEnvelope struct {
	Kind   string `json:"kind"`
	Change any    `json:"change"`
}

// sessionEnvelope carries what /api/sessions/:id/events sends, under event.
type sessionEnvelope struct {
	Kind      string             `json:"kind"`
	SessionID string             `json:"session_id"`
	Event     ports.SessionEvent `json:"event"`
}

// pageStream is one open page event stream. Only the handler's goroutine
// writes its response; other calls reach the stream through the registry by
// id and hand it requests on follows.
type pageStream struct {
	id      string
	follows chan followRequest
	// done is closed when the stream's loop has returned, so a request that
	// found the stream just as it ended does not wait on it.
	done chan struct{}
}

// followRequest asks a stream to follow sessionID ("" follows none),
// replaying its buffered events after afterSeq. The loop closes subscribed
// once it is subscribed.
type followRequest struct {
	sessionID  string
	afterSeq   int64
	subscribed chan struct{}
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
	s := &pageStream{id: hex.EncodeToString(b[:]), follows: make(chan followRequest), done: make(chan struct{})}
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
// out. The opt-in parts named in `with` ride along too: usage (the
// project's, so none when project_id is empty) and projects. It reads the
// same followers the old streams read, so it never holds back a send; when
// any of them falls behind the whole stream ends and the client reconnects.
func (h *Handler) handlePageEvents(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	// Ending the stream for a follower that fell behind releases the others.
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
	var (
		usage    <-chan *domain.UsageRecord
		projects <-chan any
		with     = map[string]bool{}
	)
	for _, kind := range strings.Split(c.QueryParam("with"), ",") {
		with[kind] = true
	}
	if with["usage"] && projectID != "" && h.usage != nil {
		if usage, err = h.usage.FollowUsage(ctx, projectID); err != nil {
			return err
		}
	}
	if with["projects"] && h.projectFeed != nil {
		if projects, err = h.followProjectEvents(ctx); err != nil {
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
	var subscribe sessionSubscriber
	if h.orchSvc != nil {
		subscribe = h.orchSvc.Subscribe
	}
	s.run(ctx, w, project, inbox, usage, projects, subscribe)
	return nil
}

// sessionSubscriber is the hub's Subscribe: the live channel, the events
// already buffered, and the call that releases the subscription.
type sessionSubscriber func(id string) (<-chan ports.SessionEvent, []ports.SessionEvent, func())

// run writes the stream's changes, and the events of the session it is
// asked to follow, until ctx ends or a project, inbox, usage or projects
// follower closes.
func (s *pageStream) run(ctx context.Context, w *echo.Response, project <-chan ports.ProjectChange, inbox <-chan ports.NotificationChange, usage <-chan *domain.UsageRecord, projects <-chan any, subscribe sessionSubscriber) {
	defer close(s.done)
	ticker := time.NewTicker(pageEventsPing)
	defer ticker.Stop()
	var (
		following string
		session   <-chan ports.SessionEvent
		unfollow  = func() {}
	)
	defer func() { unfollow() }()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.follows:
			unfollow()
			following, session, unfollow = req.sessionID, nil, func() {}
			if following == "" {
				close(req.subscribed)
				continue
			}
			ch, replay, cancel := subscribe(following)
			session, unfollow = ch, cancel
			close(req.subscribed)
			for _, ev := range replayFrom(replay, req.afterSeq) {
				writeEnvelope(w, sessionEnvelope{Kind: "session", SessionID: following, Event: ev})
			}
		case ev, ok := <-session:
			// The hub drops a slow follower's events rather than wait, and
			// closes the channel only on a release: neither ends the stream.
			if !ok {
				session = nil
				continue
			}
			writeEnvelope(w, sessionEnvelope{Kind: "session", SessionID: following, Event: ev})
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
		case record, ok := <-usage:
			if !ok {
				return
			}
			writeEnvelope(w, usageEnvelope{Kind: "usage", Record: record})
		case change, ok := <-projects:
			if !ok {
				return
			}
			writeEnvelope(w, projectsEnvelope{Kind: "projects", Change: change})
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

// handleFollowSession makes an open page event stream follow one session, or
// none when session_id is null. The stream's own loop makes the switch; the
// call answers 204 once that loop has subscribed, so every event published
// after the answer is carried.
func (h *Handler) handleFollowSession(c echo.Context) error {
	if h.orchSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration not configured")
	}
	var body struct {
		StreamID  string  `json:"stream_id"`
		SessionID *string `json:"session_id"`
		AfterSeq  int64   `json:"after_seq"`
	}
	if err := bindJSON(c, &body); err != nil {
		return err
	}
	ctx := c.Request().Context()
	s, ok := h.pageStreams.lookup(body.StreamID)
	if !ok {
		return errStreamNotFound
	}
	req := followRequest{afterSeq: body.AfterSeq, subscribed: make(chan struct{})}
	if body.SessionID != nil {
		if _, err := h.orchSvc.Get(ctx, *body.SessionID); err != nil {
			return err
		}
		req.sessionID = *body.SessionID
	}
	select {
	case s.follows <- req:
	case <-s.done:
		return errStreamNotFound
	case <-ctx.Done():
		return ctx.Err()
	}
	// The loop took the request, and closes subscribed before it can return.
	<-req.subscribed
	return c.NoContent(http.StatusNoContent)
}

var errStreamNotFound = &domain.StructuredError{Code: "STREAM_NOT_FOUND", Message: "page event stream not found"}
