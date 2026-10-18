package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/infra/querycount"
)

// DefaultSlowRequestThreshold is how long an HTTP request may take before it
// is logged as a slow request.
const DefaultSlowRequestThreshold = 300 * time.Millisecond

// SlowRequestOption configures SlowRequestLog.
type SlowRequestOption func(*slowRequests)

// WithSlowRequestThreshold sets how long a request may take before it is
// logged as a slow request. Zero or less keeps DefaultSlowRequestThreshold.
func WithSlowRequestThreshold(d time.Duration) SlowRequestOption {
	return func(s *slowRequests) {
		if d > 0 {
			s.threshold = d
		}
	}
}

// WithSlowRequestLogger sets where slow requests are logged; slog.Default()
// when unset.
func WithSlowRequestLogger(l *slog.Logger) SlowRequestOption {
	return func(s *slowRequests) { s.logger = l }
}

type slowRequests struct {
	next      http.Handler
	threshold time.Duration
	logger    *slog.Logger
}

// SlowRequestLog times every request next serves and counts the database
// calls it makes (see querycount). A request slower than the threshold logs
// one Warn line, beside the slow database call log, with its route pattern,
// its duration and its query count. Event streams and WebSockets stay open
// for as long as someone watches, so they are never slow requests.
func SlowRequestLog(next http.Handler, opts ...SlowRequestOption) http.Handler {
	s := &slowRequests{next: next, threshold: DefaultSlowRequestThreshold}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *slowRequests) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isLongLived(r) {
		s.next.ServeHTTP(w, r)
		return
	}
	start := time.Now()
	ctx, queries, stop := querycount.Track(r.Context())
	defer stop()
	route := &routePattern{}
	r = r.WithContext(context.WithValue(ctx, routePatternKey{}, route))
	s.next.ServeHTTP(w, r)
	elapsed := time.Since(start)
	if elapsed <= s.threshold || isEventStream(w.Header()) {
		return
	}
	l := s.logger
	if l == nil {
		l = slog.Default()
	}
	l.Warn("slow request", "route", route.or(r), "duration", elapsed, "queries", queries.Count())
}

// isLongLived reports whether r opens an event stream or a WebSocket.
func isLongLived(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// isEventStream reports whether a response is an event stream, for the
// stream clients that do not say they want one.
func isEventStream(h http.Header) bool {
	return strings.HasPrefix(h.Get("Content-Type"), "text/event-stream")
}

// routePattern is the route a request matched, for routers that do not
// record it on the request as http.ServeMux does.
type routePattern struct{ pattern string }

type routePatternKey struct{}

// or is the pattern the router reported, else the pattern the innermost
// http.ServeMux recorded on r, else r's path.
func (p *routePattern) or(r *http.Request) string {
	switch {
	case p.pattern != "":
		return p.pattern
	case r.Pattern != "":
		return r.Pattern
	default:
		return r.URL.Path
	}
}

// reportRoute records the echo route a request matched for SlowRequestLog.
func reportRoute(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if p, ok := c.Request().Context().Value(routePatternKey{}).(*routePattern); ok && c.Path() != "" {
			p.pattern = c.Request().Method + " " + c.Path()
		}
		return next(c)
	}
}
