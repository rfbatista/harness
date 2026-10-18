package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// One live stream per browser tab. Over plain HTTP/1.1 a browser opens at
// most six connections to a host, across all its tabs; with an EventSource
// per feed (the project, the project list, the inbox, a session, usage) two
// tabs used them all and every other request queued behind them. A tab
// instead opens GET /api/stream once and adds or drops feeds on it with
// POST /api/stream/follow and /api/stream/unfollow. Each feed is named by the
// path of its own SSE endpoint, which stays for the clients that use it
// (tui-client).
//
// The stream's first event is {"stream": id}. Each change after it is
// {"path": p, "data": change}; {"path": p, "end": true} says the feed at p
// ended (its follower fell behind) and must be followed again.

// streamMessage is one event on a tab's stream.
type streamMessage struct {
	Stream string `json:"stream,omitempty"`
	Path   string `json:"path,omitempty"`
	Data   any    `json:"data,omitempty"`
	End    bool   `json:"end,omitempty"`
}

// streamRegistry holds the open streams by id. Its zero value is ready.
type streamRegistry struct {
	mu   sync.Mutex
	byID map[string]*liveStream
}

func (r *streamRegistry) open(ctx context.Context) *liveStream {
	var b [16]byte
	_, _ = rand.Read(b[:])
	s := &liveStream{
		id:      hex.EncodeToString(b[:]),
		ctx:     ctx,
		out:     make(chan streamMessage, 64),
		follows: map[string]*follow{},
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID == nil {
		r.byID = map[string]*liveStream{}
	}
	r.byID[s.id] = s
	return s
}

func (r *streamRegistry) close(s *liveStream) {
	r.mu.Lock()
	delete(r.byID, s.id)
	r.mu.Unlock()
	s.unfollowAll()
}

func (r *streamRegistry) get(id string) (*liveStream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byID[id]; ok {
		return s, nil
	}
	return nil, &domain.StructuredError{Code: "STREAM_NOT_FOUND", Message: "no open stream " + id}
}

// liveStream is one tab's connection and the feeds it follows.
type liveStream struct {
	id  string
	ctx context.Context // the GET request's: every follow ends with it
	out chan streamMessage

	mu      sync.Mutex
	follows map[string]*follow
}

type follow struct{ cancel context.CancelFunc }

// follow starts relaying the feed at path; following a path twice is one follow.
func (s *liveStream) follow(h *Handler, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.follows[path]; ok {
		return nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	changes, err := h.feedFor(ctx, path)
	if err != nil {
		cancel()
		return err
	}
	f := &follow{cancel: cancel}
	s.follows[path] = f
	go s.relay(ctx, path, f, changes)
	return nil
}

func (s *liveStream) relay(ctx context.Context, path string, f *follow, changes <-chan any) {
	send := func(m streamMessage) bool {
		select {
		case s.out <- m:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for change := range changes {
		if !send(streamMessage{Path: path, Data: change}) {
			return
		}
	}
	// The feed closed on its own: the follower fell behind.
	s.mu.Lock()
	if s.follows[path] == f {
		delete(s.follows, path)
	}
	s.mu.Unlock()
	if ctx.Err() == nil {
		send(streamMessage{Path: path, End: true})
	}
	f.cancel()
}

func (s *liveStream) unfollow(path string) {
	s.mu.Lock()
	f, ok := s.follows[path]
	delete(s.follows, path)
	s.mu.Unlock()
	if ok {
		f.cancel()
	}
}

func (s *liveStream) unfollowAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for path, f := range s.follows {
		f.cancel()
		delete(s.follows, path)
	}
}

// handleStream opens a tab's stream: the {"stream": id} event, then each
// change of the feeds followed on it, and a comment every 15s to keep the
// connection alive, until the request ends.
func (h *Handler) handleStream(c echo.Context) error {
	s := h.streams.open(c.Request().Context())
	defer h.streams.close(s)

	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writeStreamMessage(w, streamMessage{Stream: s.id})
	w.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case m := <-s.out:
			writeStreamMessage(w, m)
			w.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}

func writeStreamMessage(w http.ResponseWriter, m streamMessage) {
	data, _ := json.Marshal(m)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

type streamFollowRequest struct {
	Stream string `json:"stream"`
	Path   string `json:"path"`
}

// handleStreamFollow adds the feed at path to an open stream.
func (h *Handler) handleStreamFollow(c echo.Context) error {
	var req streamFollowRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	s, err := h.streams.get(req.Stream)
	if err != nil {
		return err
	}
	if err := s.follow(h, req.Path); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{})
}

// handleStreamUnfollow drops the feed at path from an open stream; dropping
// one it does not follow does nothing.
func (h *Handler) handleStreamUnfollow(c echo.Context) error {
	var req streamFollowRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	s, err := h.streams.get(req.Stream)
	if err != nil {
		return err
	}
	s.unfollow(req.Path)
	return c.JSON(http.StatusOK, map[string]any{})
}

// feedFor resolves a feed's path — that of its own SSE endpoint, relative to
// /api — to its changes, as that endpoint would send them, until ctx ends.
func (h *Handler) feedFor(ctx context.Context, path string) (<-chan any, error) {
	u, err := url.Parse(path)
	if err != nil {
		return nil, unknownFeed(path)
	}
	q := u.Query()
	switch parts := strings.Split(strings.Trim(u.Path, "/"), "/"); {
	case u.Path == "/events":
		return h.followProject(ctx, q.Get("project_id"))
	case u.Path == "/project_events":
		return h.followCatalog(ctx)
	case u.Path == "/notifications/events":
		return h.followNotifications(ctx)
	case len(parts) == 4 && parts[0] == "projects" && parts[2] == "usage" && parts[3] == "events" && parts[1] != "":
		return h.followUsage(ctx, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "events" && parts[1] != "":
		var from int64
		if v := q.Get("from_seq"); v != "" {
			from, _ = strconv.ParseInt(v, 10, 64)
		}
		return h.followSession(ctx, parts[1], from)
	}
	return nil, unknownFeed(path)
}

func unknownFeed(path string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: "no feed at " + path}
}

// relayAs turns a feed's typed changes into their wire values, closing the
// result when changes closes or ctx ends.
func relayAs[T any](ctx context.Context, changes <-chan T, wire func(T) any) <-chan any {
	out := make(chan any)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ch, ok := <-changes:
				if !ok {
					return
				}
				select {
				case out <- wire(ch):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}
