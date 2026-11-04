//go:build !windows

package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every project page says how long it took to build: its page server time
// and where it went, in the Server-Timing header and in one log line.
func TestEveryProjectPageReportsItsPageServerTime(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(6)
	base := "/projects/" + b.project
	pages := map[string]string{
		"board":     base,
		"task page": base + "/tasks/" + b.tickets[0],
		"settings":  base + "/settings",
		"documents": base + "/documents",
		"code":      base + "/repositories/" + b.repo + "/code",
		"design":    base + "/design",
		"usage":     base + "/usage",
		"reviews":   base + "/reviews",
	}
	for name, path := range pages {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(prev)

			rec := httptest.NewRecorder()
			b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil).WithContext(context.Background()))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d", path, rec.Code)
			}
			m := parseServerTiming(t, rec.Header().Get("Server-Timing"))
			for _, k := range []string{"total", "db", "git", "render", "frame"} {
				if _, ok := m[k]; !ok {
					t.Fatalf("Server-Timing %q has no %s", rec.Header().Get("Server-Timing"), k)
				}
			}
			if m["render"] > m["total"] || m["frame"] > m["total"] {
				t.Errorf("render %v and frame %v must each be within total %v", m["render"], m["frame"], m["total"])
			}
			line := logs.String()
			for _, want := range []string{"page time", "route=", "project=" + b.project, "total=", "db=", "git=", "git_calls=", "render=", "frame="} {
				if !strings.Contains(line, want) {
					t.Errorf("log line lacks %q:\n%s", want, line)
				}
			}
		})
	}
}

// parseServerTiming reads "name;dur=1.5, name;desc=..;dur=2" into durations.
func parseServerTiming(t *testing.T, header string) map[string]time.Duration {
	t.Helper()
	out := map[string]time.Duration{}
	for _, metric := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(metric), ";")
		name := parts[0]
		for _, p := range parts[1:] {
			if v, ok := strings.CutPrefix(p, "dur="); ok {
				ms, err := strconv.ParseFloat(v, 64)
				if err != nil {
					t.Fatalf("Server-Timing %q: %v", header, err)
				}
				out[name] = time.Duration(ms * float64(time.Millisecond))
			}
		}
	}
	return out
}
