package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
)

// slowRequestLines records the slow request lines logged.
type slowRequestLines struct {
	mu    sync.Mutex
	lines []map[string]string
}

func (l *slowRequestLines) Enabled(context.Context, slog.Level) bool { return true }
func (l *slowRequestLines) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "slow request" {
		return nil
	}
	line := map[string]string{"level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool {
		line[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	return nil
}
func (l *slowRequestLines) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *slowRequestLines) WithGroup(string) slog.Handler      { return l }

func (l *slowRequestLines) all() []map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]map[string]string(nil), l.lines...)
}

// pageMux serves GET /pages/{n}: a page that reads the settings n times
// through a repository that takes no context, then takes wait to answer.
func pageMux(t *testing.T, wait time.Duration) http.Handler {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	settings := sqlite.NewSettingsRepository(db)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /pages/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		for range n {
			if _, err := settings.Get("theme"); err != nil {
				t.Error(err)
			}
		}
		time.Sleep(wait)
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func serve(h http.Handler, r *http.Request) {
	h.ServeHTTP(httptest.NewRecorder(), r)
}

// A slow page that makes N database calls logs one line naming its route
// pattern, how long it took and N.
func TestASlowRequestLogsItsRouteDurationAndQueryCount(t *testing.T) {
	logs := &slowRequestLines{}
	h := SlowRequestLog(pageMux(t, 30*time.Millisecond), WithSlowRequestThreshold(20*time.Millisecond), WithSlowRequestLogger(slog.New(logs)))

	serve(h, httptest.NewRequest(http.MethodGet, "/pages/4", nil))

	lines := logs.all()
	if len(lines) != 1 {
		t.Fatalf("slow request lines = %v, want one", lines)
	}
	line := lines[0]
	if line["route"] != "GET /pages/{n}" {
		t.Errorf("route = %q, want GET /pages/{n}", line["route"])
	}
	if line["queries"] != "4" {
		t.Errorf("queries = %q, want 4", line["queries"])
	}
	if d, err := time.ParseDuration(line["duration"]); err != nil || d < 30*time.Millisecond {
		t.Errorf("duration = %q, want at least 30ms", line["duration"])
	}
	if line["level"] != slog.LevelWarn.String() {
		t.Errorf("level = %q, want WARN, as the slow database call log", line["level"])
	}
}

// A request within the threshold logs nothing.
func TestAFastRequestLogsNothing(t *testing.T) {
	logs := &slowRequestLines{}
	h := SlowRequestLog(pageMux(t, 0), WithSlowRequestThreshold(time.Hour), WithSlowRequestLogger(slog.New(logs)))

	serve(h, httptest.NewRequest(http.MethodGet, "/pages/3", nil))

	if lines := logs.all(); len(lines) != 0 {
		t.Errorf("slow request lines = %v, want none", lines)
	}
}

// A threshold of zero or less keeps the default, which a quick request is
// well within.
func TestANonPositiveThresholdKeepsTheDefault(t *testing.T) {
	for _, threshold := range []time.Duration{0, -time.Second} {
		logs := &slowRequestLines{}
		h := SlowRequestLog(pageMux(t, 5*time.Millisecond), WithSlowRequestThreshold(threshold), WithSlowRequestLogger(slog.New(logs)))

		serve(h, httptest.NewRequest(http.MethodGet, "/pages/1", nil))

		if lines := logs.all(); len(lines) != 0 {
			t.Errorf("threshold %v: slow request lines = %v, want none (default %v)", threshold, lines, DefaultSlowRequestThreshold)
		}
	}
}

// Requests served at the same time each count only their own database calls.
func TestConcurrentSlowRequestsEachCountTheirOwnQueries(t *testing.T) {
	logs := &slowRequestLines{}
	h := SlowRequestLog(pageMux(t, 30*time.Millisecond), WithSlowRequestThreshold(20*time.Millisecond), WithSlowRequestLogger(slog.New(logs)))

	var wg sync.WaitGroup
	for n := 1; n <= 6; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			serve(h, httptest.NewRequest(http.MethodGet, "/pages/"+strconv.Itoa(n*5), nil))
		}()
	}
	wg.Wait()

	got := map[string]int{}
	for _, line := range logs.all() {
		got[line["queries"]]++
	}
	for n := 1; n <= 6; n++ {
		if q := strconv.Itoa(n * 5); got[q] != 1 {
			t.Errorf("lines naming %s queries = %d, want 1 (all: %v)", q, got[q], logs.all())
		}
	}
}

// A JSON API request names its echo route.
func TestASlowAPIRequestNamesItsAPIRoute(t *testing.T) {
	logs := &slowRequestLines{}
	h := SlowRequestLog(NewRouter(NewHandler(Services{})), WithSlowRequestThreshold(time.Nanosecond), WithSlowRequestLogger(slog.New(logs)))

	serve(h, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	lines := logs.all()
	if len(lines) != 1 || lines[0]["route"] != "GET /api/health" {
		t.Fatalf("slow request lines = %v, want one with route GET /api/health", lines)
	}
}

// Long-lived streams and WebSockets are never slow requests, however long
// they stay open.
func TestStreamsAndWebSocketsNeverLogAsSlowRequests(t *testing.T) {
	logs := &slowRequestLines{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		time.Sleep(10 * time.Millisecond)
	})
	mux.HandleFunc("GET /terminal", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
	})
	h := SlowRequestLog(mux, WithSlowRequestThreshold(time.Nanosecond), WithSlowRequestLogger(slog.New(logs)))

	serve(h, httptest.NewRequest(http.MethodGet, "/events", nil))
	ws := httptest.NewRequest(http.MethodGet, "/terminal", nil)
	ws.Header.Set("Connection", "Upgrade")
	ws.Header.Set("Upgrade", "websocket")
	serve(h, ws)

	if lines := logs.all(); len(lines) != 0 {
		t.Errorf("slow request lines = %v, want none", lines)
	}
}

// The page event stream logs nothing, however long a tab keeps it open.
func TestThePageEventStreamNeverLogsAsASlowRequest(t *testing.T) {
	logs := &slowRequestLines{}
	h := SlowRequestLog(NewRouter(NewHandler(Services{})), WithSlowRequestThreshold(time.Nanosecond), WithSlowRequestLogger(slog.New(logs)))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	serve(h, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/stream", nil))

	if lines := logs.all(); len(lines) != 0 {
		t.Errorf("slow request lines = %v, want none", lines)
	}
}

// A request an EventSource makes is a stream even when it ends before its
// handler answers (the browser gave up, say).
func TestAnEventSourceRequestNeverLogsAsASlowRequest(t *testing.T) {
	logs := &slowRequestLines{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
	})
	h := SlowRequestLog(mux, WithSlowRequestThreshold(time.Nanosecond), WithSlowRequestLogger(slog.New(logs)))

	r := httptest.NewRequest(http.MethodGet, "/events", nil)
	r.Header.Set("Accept", "text/event-stream")
	serve(h, r)

	if lines := logs.all(); len(lines) != 0 {
		t.Errorf("slow request lines = %v, want none", lines)
	}
}
