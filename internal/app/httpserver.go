package app

import (
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"
)

// httpReadHeaderTimeout is how long a client may take to send its request
// headers before the server drops the connection, so a client that never
// finishes them cannot hold a connection open forever.
const httpReadHeaderTimeout = 10 * time.Second

// profilerPrefix is where Go's profiler answers, goroutine dump included.
const profilerPrefix = "/debug/pprof/"

// newHTTPServer is the UI and API server: headers are bounded by
// httpReadHeaderTimeout, and there is no write timeout, as the page event
// streams and terminals write for as long as someone follows them.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: httpReadHeaderTimeout}
}

// withProfiler serves Go's profiler under profilerPrefix in front of next,
// to loopback peers only, so a freeze can be captured while it happens.
// Turned off, next answers those paths like any other. The handlers are
// mounted one by one: the server never serves http.DefaultServeMux, where
// net/http/pprof also registers itself.
func withProfiler(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	profiler := http.NewServeMux()
	profiler.HandleFunc(profilerPrefix, pprof.Index)
	profiler.HandleFunc(profilerPrefix+"cmdline", pprof.Cmdline)
	profiler.HandleFunc(profilerPrefix+"profile", pprof.Profile)
	profiler.HandleFunc(profilerPrefix+"symbol", pprof.Symbol)
	profiler.HandleFunc(profilerPrefix+"trace", pprof.Trace)

	guarded := loopbackPeersOnly(profiler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, profilerPrefix) {
			guarded.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackPeersOnly refuses every peer that is not this machine. It reads
// the connection's own address, never a forwarded header a client can set.
func loopbackPeersOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "the profiler answers this machine only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
