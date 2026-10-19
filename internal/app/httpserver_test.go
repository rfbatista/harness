package app

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fallthroughHandler stands in for the rest of the server: it answers 418 so
// a test can tell it apart from the profiler.
var fallthroughHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusTeapot)
})

func serveFrom(h http.Handler, remoteAddr, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheProfilerAnswersLoopbackPeers(t *testing.T) {
	h := withProfiler(true, fallthroughHandler)
	for _, peer := range []string{"127.0.0.1:50000", "[::1]:50000"} {
		index := serveFrom(h, peer, "/debug/pprof/")
		if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "goroutine") {
			t.Errorf("index from %s = %d, want 200 listing the goroutine profile", peer, index.Code)
		}
		dump := serveFrom(h, peer, "/debug/pprof/goroutine?debug=2")
		if dump.Code != http.StatusOK || !strings.Contains(dump.Body.String(), "goroutine ") {
			t.Errorf("goroutine dump from %s = %d, want 200 with stacks", peer, dump.Code)
		}
	}
}

func TestTheProfilerRefusesOtherPeers(t *testing.T) {
	h := withProfiler(true, fallthroughHandler)
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/goroutine?debug=2", "/debug/pprof/cmdline"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.168.1.20:50000"
		// A forwarded header claiming loopback must not open the door.
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s from a non-loopback peer = %d, want 403", path, rec.Code)
		}
	}
}

func TestTheProfilerLeavesOtherPathsToTheServer(t *testing.T) {
	h := withProfiler(true, fallthroughHandler)
	if rec := serveFrom(h, "192.168.1.20:50000", "/api/health"); rec.Code != http.StatusTeapot {
		t.Errorf("/api/health = %d, want the rest of the server to answer it", rec.Code)
	}
}

func TestTheProfilerTurnedOffIsNotServed(t *testing.T) {
	h := withProfiler(false, fallthroughHandler)
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/goroutine?debug=2"} {
		if rec := serveFrom(h, "127.0.0.1:50000", path); rec.Code != http.StatusTeapot {
			t.Errorf("%s with the profiler off = %d, want it left to the rest of the server", path, rec.Code)
		}
	}
}

func TestTheHTTPServerHasAHeaderTimeoutAndNoWriteTimeout(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", fallthroughHandler)
	if srv.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout = %v, want a bound on slow headers", srv.ReadHeaderTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want none so streams stay open", srv.WriteTimeout)
	}
}

// TestAPartialHeaderIsDroppedWhileAStreamStaysOpen runs the server as
// newHTTPServer builds it, with the header timeout shortened so the test
// stays quick: a client that never finishes its headers is closed, and a
// stream that keeps writing outlives the same timeout.
func TestAPartialHeaderIsDroppedWhileAStreamStaysOpen(t *testing.T) {
	const timeout = 200 * time.Millisecond
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			io.WriteString(w, "data: tick\n\n")
			flusher.Flush()
			time.Sleep(timeout)
		}
	})
	srv := newHTTPServer("127.0.0.1:0", stream)
	srv.ReadHeaderTimeout = timeout
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	slow, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	io.WriteString(slow, "GET / HTTP/1.1\r\nHost: x\r\n")
	slow.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	if _, err := io.ReadAll(slow); err != nil {
		t.Fatalf("partial-header connection: %v, want the server to close it", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("partial-header connection closed after %v, want about %v", took, timeout)
	}

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ticks := 0
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if sc.Text() == "data: tick" {
			ticks++
		}
	}
	if ticks != 5 {
		t.Errorf("stream delivered %d ticks over %v, want all 5 (err %v)", ticks, 5*timeout, sc.Err())
	}
}
