package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/application/orchestration"
)

func ctxWith(t *testing.T, target, lastEventID string) echo.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	return echo.New().NewContext(req, httptest.NewRecorder())
}

func TestResumeSeq(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		lastEventID string
		want        int64
	}{
		{"no resume info", "/sessions/s1/events", "", 0},
		{"from_seq only", "/sessions/s1/events?from_seq=42", "", 42},
		{"last-event-id only", "/sessions/s1/events", "17", 17},
		// On an EventSource auto-reconnect the URL keeps its original
		// from_seq while the header carries the true high-water mark.
		{"header wins over stale param", "/sessions/s1/events?from_seq=10", "350", 350},
		{"malformed param ignored", "/sessions/s1/events?from_seq=abc", "", 0},
		{"malformed header falls back to param", "/sessions/s1/events?from_seq=7", "xyz", 7},
		{"negative param is kept as-is", "/sessions/s1/events?from_seq=-5", "", -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resumeSeq(ctxWith(t, tt.target, tt.lastEventID)); got != tt.want {
				t.Fatalf("resumeSeq = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReplayFrom(t *testing.T) {
	events := []orchestration.SessionEvent{
		{Seq: 1, Type: "status"},
		{Seq: 2, Type: "output"},
		{Seq: 3, Type: "tool_use"},
	}

	t.Run("zero returns everything", func(t *testing.T) {
		if got := replayFrom(events, 0); len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
	})

	t.Run("skips events at or below the resume point", func(t *testing.T) {
		got := replayFrom(events, 2)
		if len(got) != 1 || got[0].Seq != 3 {
			t.Fatalf("got %+v, want only seq 3", got)
		}
	})

	t.Run("resume past the end returns empty, not nil-panic", func(t *testing.T) {
		if got := replayFrom(events, 99); len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})

	t.Run("empty input is safe", func(t *testing.T) {
		if got := replayFrom(nil, 5); len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})
}
