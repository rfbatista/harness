package web

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// newBrowserServer starts a test server for h with the tab's stream bridged
// over it (withStream). Its streams end when the server closes: a page holds
// one open, and httptest.Server.Close would otherwise wait for the browser to
// drop it.
func newBrowserServer(h http.Handler) *httptest.Server {
	b := withStream(h)
	srv := httptest.NewUnstartedServer(b)
	srv.Listener = &closingListener{Listener: srv.Listener, onClose: b.shutdown}
	srv.Start()
	return srv
}

// closingListener tells the bridge when the server stops listening, the first
// thing httptest.Server.Close does.
type closingListener struct {
	net.Listener
	once    sync.Once
	onClose func()
}

func (l *closingListener) Close() error {
	l.once.Do(l.onClose)
	return l.Listener.Close()
}

// withStream serves the tab's one live stream — GET /api/stream and its
// follow and unfollow — over the per-path SSE handlers a browser test
// mounts, the way the real server (httpapi/stream.go) serves it over its
// feeds: each follow requests GET /api<path> from next and relays its data
// lines as {"path", "data"}, and {"path", "end": true} when it ends.
func withStream(next http.Handler) *streamBridge {
	return &streamBridge{next: next, streams: map[string]*bridgedStream{}, done: make(chan struct{})}
}

type streamBridge struct {
	next    http.Handler
	mu      sync.Mutex
	streams map[string]*bridgedStream
	done    chan struct{} // closed when the server shuts down
}

func (b *streamBridge) shutdown() { close(b.done) }

type bridgedStream struct {
	ctx     context.Context
	out     chan string
	mu      sync.Mutex
	follows map[string]context.CancelFunc
}

func (b *streamBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/stream":
		b.serveStream(w, r)
	case "/api/stream/follow", "/api/stream/unfollow":
		var req struct{ Stream, Path string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		b.mu.Lock()
		s := b.streams[req.Stream]
		b.mu.Unlock()
		if s == nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"no stream","code":"STREAM_NOT_FOUND"}`)
			return
		}
		status := http.StatusOK
		if r.URL.Path == "/api/stream/follow" {
			status = b.follow(s, req.Path, r)
		} else {
			s.mu.Lock()
			if cancel := s.follows[req.Path]; cancel != nil {
				cancel()
				delete(s.follows, req.Path)
			}
			s.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, "{}")
	default:
		b.next.ServeHTTP(w, r)
	}
}

func (b *streamBridge) serveStream(w http.ResponseWriter, r *http.Request) {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	id := hex.EncodeToString(raw[:])
	s := &bridgedStream{ctx: r.Context(), out: make(chan string, 64), follows: map[string]context.CancelFunc{}}
	b.mu.Lock()
	b.streams[id] = s
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.streams, id)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, "data: {\"stream\":%q}\n\n", id)
	w.(http.Flusher).Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-b.done:
			return
		case line := <-s.out:
			fmt.Fprintf(w, "data: %s\n\n", line)
			w.(http.Flusher).Flush()
		}
	}
}

// follow starts GET /api<path> on next and answers with its status once it
// has one: 200 when the feed is open.
func (b *streamBridge) follow(s *bridgedStream, path string, r *http.Request) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.follows[path] != nil {
		return http.StatusOK
	}
	ctx, cancel := context.WithCancel(s.ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api"+path, nil)
	if err != nil {
		cancel()
		return http.StatusBadRequest
	}
	req.Header = r.Header.Clone()
	pr, pw := io.Pipe()
	rw := &pipeWriter{w: pw, header: http.Header{}, started: make(chan struct{})}
	go func() {
		b.next.ServeHTTP(rw, req)
		rw.start(http.StatusOK)
		pw.Close()
	}()
	<-rw.started
	if rw.status != http.StatusOK {
		cancel()
		go io.Copy(io.Discard, pr)
		return rw.status
	}
	s.follows[path] = cancel
	go func() {
		scan := bufio.NewScanner(pr)
		for scan.Scan() {
			data, ok := strings.CutPrefix(scan.Text(), "data: ")
			if !ok {
				continue
			}
			msg, _ := json.Marshal(map[string]any{"path": path, "data": json.RawMessage(data)})
			select {
			case s.out <- string(msg):
			case <-ctx.Done():
				go io.Copy(io.Discard, pr)
				return
			}
		}
		if ctx.Err() == nil {
			msg, _ := json.Marshal(map[string]any{"path": path, "end": true})
			select {
			case s.out <- string(msg):
			case <-ctx.Done():
			}
		}
	}()
	return http.StatusOK
}

// pipeWriter is the ResponseWriter a bridged feed writes into.
type pipeWriter struct {
	w       *io.PipeWriter
	header  http.Header
	once    sync.Once
	status  int
	started chan struct{}
}

func (p *pipeWriter) start(status int) {
	p.once.Do(func() {
		p.status = status
		close(p.started)
	})
}

func (p *pipeWriter) Header() http.Header    { return p.header }
func (p *pipeWriter) WriteHeader(status int) { p.start(status) }
func (p *pipeWriter) Write(b []byte) (int, error) {
	p.start(http.StatusOK)
	return p.w.Write(b)
}
func (p *pipeWriter) Flush() {}
