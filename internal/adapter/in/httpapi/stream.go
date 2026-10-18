package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
//
// Backpressure (ADR 0012) is per feed: each follow queues its changes on its
// own, up to followQueueLen. A follow whose queue is full stops reading its
// feed until the writer makes room, so it never holds back another follow.
// Every live feed fans out without blocking and closes a follower that falls
// behind, so a feed that keeps outrunning the connection closes, and its end
// is sent; a backlog (a session's stored events, read from the log on a
// resume) waits instead and arrives at the pace the client reads it. A
// session feed keeps reading its hub subscription while its follow waits,
// merging the terminal output deltas that arrive meanwhile (coalesce in
// sessions.go), so a burst of output does not make it fall behind. The
// writer takes one change from each follow in turn, in each feed's order. A connection that cannot take a write within
// the registry's write timeout (a dead or stuck client) closes the stream,
// and with it every follow.

// followQueueLen is how many changes a follow may have waiting to be written
// before it stops reading its feed.
const followQueueLen = 256

// defaultStreamWriteTimeout is how long one write to a stream may take before
// the stream is closed.
const defaultStreamWriteTimeout = 10 * time.Second

// streamMessage is one event on a tab's stream.
type streamMessage struct {
	Stream string `json:"stream,omitempty"`
	Path   string `json:"path,omitempty"`
	Data   any    `json:"data,omitempty"`
	End    bool   `json:"end,omitempty"`
}

// streamRegistry holds the open streams by id. Its zero value is ready.
type streamRegistry struct {
	// writeTimeout bounds each write to a stream; zero means
	// defaultStreamWriteTimeout.
	writeTimeout time.Duration

	mu   sync.Mutex
	byID map[string]*liveStream
}

func (r *streamRegistry) open(ctx context.Context) *liveStream {
	var b [16]byte
	_, _ = rand.Read(b[:])
	s := &liveStream{
		id:      hex.EncodeToString(b[:]),
		ctx:     ctx,
		wake:    make(chan struct{}, 1),
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

func (r *streamRegistry) timeout() time.Duration {
	if r.writeTimeout > 0 {
		return r.writeTimeout
	}
	return defaultStreamWriteTimeout
}

// liveStream is one tab's connection and the feeds it follows.
type liveStream struct {
	id   string
	ctx  context.Context // the GET request's: every follow ends with it
	wake chan struct{}   // a follow has something to write

	mu      sync.Mutex
	follows map[string]*follow
	ready   []*follow // the follows with queued messages, in writing turn
}

// follow is one feed relayed on a stream. Its queue and done are guarded by
// the stream's mu.
type follow struct {
	path   string
	cancel context.CancelFunc
	queue  []streamMessage
	done   bool          // dropped: nothing more is queued, and nothing queued is written
	room   chan struct{} // the writer took from a full queue
}

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
	f := &follow{path: path, cancel: cancel, room: make(chan struct{}, 1)}
	s.follows[path] = f
	go s.relay(ctx, f, changes)
	return nil
}

// relay queues each change of the feed for the writer until the feed closes
// or the follow is dropped. It waits only for room in its own queue.
func (s *liveStream) relay(ctx context.Context, f *follow, changes <-chan any) {
	for {
		select {
		case <-ctx.Done():
			return
		case change, ok := <-changes:
			if !ok {
				// The feed closed on its own: its follower fell behind.
				s.end(f)
				return
			}
			if !s.enqueue(ctx, f, streamMessage{Path: f.path, Data: change}) {
				return
			}
		}
	}
}

// enqueue queues m on f, waiting while f's queue is full. It says whether f
// still follows its feed.
func (s *liveStream) enqueue(ctx context.Context, f *follow, m streamMessage) bool {
	for {
		s.mu.Lock()
		if f.done {
			s.mu.Unlock()
			return false
		}
		if len(f.queue) < followQueueLen {
			s.push(f, m)
			s.mu.Unlock()
			s.signal()
			return true
		}
		s.mu.Unlock()
		select {
		case <-f.room:
		case <-ctx.Done():
			return false
		}
	}
}

// end drops f and queues its end in place of whatever it had queued, unless
// f was already dropped (unfollowed, or the stream closed).
func (s *liveStream) end(f *follow) {
	s.mu.Lock()
	if f.done {
		s.mu.Unlock()
		return
	}
	s.drop(f)
	f.queue = nil
	s.push(&follow{path: f.path}, streamMessage{Path: f.path, End: true})
	s.mu.Unlock()
	s.signal()
}

// push appends m to f's queue, giving f a writing turn if it had none. The
// caller holds mu.
func (s *liveStream) push(f *follow, m streamMessage) {
	if len(f.queue) == 0 {
		s.ready = append(s.ready, f)
	}
	f.queue = append(f.queue, m)
}

// drop stops f and forgets it. The caller holds mu.
func (s *liveStream) drop(f *follow) {
	f.done = true
	f.cancel()
	if s.follows[f.path] == f {
		delete(s.follows, f.path)
	}
}

func (s *liveStream) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// next takes the next message to write: the head of the queue of the follow
// whose turn it is, which then goes to the back of the line if it has more.
func (s *liveStream) next() (streamMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.ready) > 0 {
		f := s.ready[0]
		s.ready = s.ready[1:]
		if f.done {
			// Dropped while it waited: its queue is not written.
			f.queue = nil
			continue
		}
		full := len(f.queue) >= followQueueLen
		m := f.queue[0]
		f.queue = f.queue[1:]
		if len(f.queue) > 0 {
			s.ready = append(s.ready, f)
		}
		if full {
			select {
			case f.room <- struct{}{}:
			default:
			}
		}
		return m, true
	}
	return streamMessage{}, false
}

func (s *liveStream) unfollow(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.follows[path]; ok {
		s.drop(f)
	}
}

func (s *liveStream) unfollowAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.follows {
		s.drop(f)
	}
}

// handleStream opens a tab's stream: the {"stream": id} event, then each
// change of the feeds followed on it, and a comment every 15s to keep the
// connection alive, until the request ends or a write fails or times out.
func (h *Handler) handleStream(c echo.Context) error {
	s := h.streams.open(c.Request().Context())
	defer h.streams.close(s)

	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	// The controller reaches the connection under echo's writer, whose Flush
	// would swallow the error of a failed flush.
	rc := http.NewResponseController(w.Writer)
	timeout := h.streams.timeout()
	// write sends one event, failing if the client does not take it within
	// timeout. No lock is held while it runs.
	write := func(event []byte) error {
		if err := rc.SetWriteDeadline(time.Now().Add(timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if _, err := w.Write(event); err != nil {
			return err
		}
		if err := rc.Flush(); err != nil {
			return err
		}
		if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		return nil
	}
	if write(streamEvent(streamMessage{Stream: s.id})) != nil {
		return nil
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-s.wake:
			for m, ok := s.next(); ok; m, ok = s.next() {
				if write(streamEvent(m)) != nil {
					// A dead or stuck client: returning closes the stream
					// and every follow on it.
					return nil
				}
			}
		case <-ticker.C:
			if write([]byte(": ping\n\n")) != nil {
				return nil
			}
		}
	}
}

func streamEvent(m streamMessage) []byte {
	data, _ := json.Marshal(m)
	return fmt.Appendf(nil, "data: %s\n\n", data)
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
