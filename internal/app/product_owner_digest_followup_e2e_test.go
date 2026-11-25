package app

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/domain"
)

// The digest's follow-up to the review (ADR 0113): a failed digest run is
// tried at most three times for a due time, and the date a page reads from is
// checked.

// syncBuffer is a log sink that the scheduler's goroutine and the test share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureLog sends the default logger to a buffer until the test ends.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

func (e *poE2E) waitLaunches(n int) {
	e.t.Helper()
	eventuallyPO(e.t, "the digest run is launched", func() string {
		if got := len(e.launches()); got != n {
			return fmt.Sprintf("%d launches, want %d", got, n)
		}
		return ""
	})
}

func TestADigestRunThatKeepsFailingIsTriedThreeTimesAndTheNextDueTimeCoversThePeriod(t *testing.T) {
	logs := captureLog(t)
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Moved on day one", domain.TicketStatusReview, clk.at(1, 6, 0))
	e.swapClaude(failingHeadlessClaude)

	clk.set(clk.at(1, 9, 0))
	e.waitLaunches(1)
	clk.set(clk.at(0, 9, 11))
	e.waitLaunches(2)
	// A restart while the second run is on would cut it, and a cut run is no
	// failed try: wait until the second failure is logged.
	eventuallyPO(t, "the second failed run", func() string {
		if n := strings.Count(logs.String(), "retrying later"); n != 2 {
			return fmt.Sprintf("%d failures logged, want 2", n)
		}
		return ""
	})

	// A restart does not reset the count: the third try is the last.
	e.restart()
	e.waitLaunches(3)
	clk.set(clk.at(0, 9, 22))
	settle()
	clk.set(clk.at(0, 9, 33))
	e.restart()
	settle()
	if n := len(e.launches()); n != 3 {
		t.Fatalf("claude was launched %d times for one due time, want 3", n)
	}
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests after three failed runs, want none", n)
	}
	if n := strings.Count(logs.String(), "gave up"); n != 1 {
		t.Errorf("the failure was logged %d times as given up, want once:\n%s", n, logs.String())
	}

	// The next due time covers the whole period, both days.
	e.moveTask("Moved on day two", domain.TicketStatusReview, clk.at(1, 6, 0))
	e.swapClaude(fakeHeadlessClaude)
	clk.set(clk.at(1, 9, 0))
	d := e.waitDigests(1).Digests[0]
	if len(d.Items) != 2 {
		t.Errorf("items = %+v, want the moves of both days", d.Items)
	}
}

func TestDigestsBeforeAMalformedDateIsRefused(t *testing.T) {
	e, _ := newDigestE2E(t)
	for _, before := range []string{"yesterday", "2030-13-40", "2030-1-7", "2030-01-07T00:00:00Z"} {
		var page poDigestPage
		if status, code := e.get("product_owner_digests?project_id="+e.project+"&before="+before, &page); status != 400 || code != "INVALID_INPUT" {
			t.Errorf("before=%q = %d %s, want 400 INVALID_INPUT", before, status, code)
		}
	}
	if page := e.digests("2030-01-07"); len(page.Digests) != 0 {
		t.Errorf("a well-formed date = %+v, want an empty page", page)
	}
}

func TestACatchUpDigestIsDatedByTheTimeItCoversSoPagingKeepsEveryDate(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Day one", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 9, 0))
	e.waitDigests(1)

	// The server is down over the next due time; it boots before the one after.
	e.moveTask("While down", domain.TicketStatusReview, clk.at(1, 6, 0))
	e.feed.stop()
	e.srv.Close()
	if err := e.app.Stop(e.ctx); err != nil {
		t.Fatal(err)
	}
	clk.set(clk.at(2, 7, 0))
	e.boot()
	e.serve()
	catchUp := e.waitDigests(2).Digests[0]
	missed := clk.at(-1, 8, 30).Format("2006-01-02")
	if catchUp.Date != missed {
		t.Fatalf("catch-up date = %s, want the missed day %s", catchUp.Date, missed)
	}

	// The same calendar day's own time then passes with a change.
	e.moveTask("Today", domain.TicketStatusReview, clk.at(0, 7, 30))
	clk.set(clk.at(0, 9, 0))
	page := e.waitDigests(3)
	today := clk.Now().Format("2006-01-02")
	dates := []string{page.Digests[0].Date, page.Digests[1].Date, page.Digests[2].Date}
	if dates[0] != today || dates[1] != missed || dates[0] == dates[1] || dates[1] == dates[2] {
		t.Fatalf("dates = %v, want today, the missed day and the first day, all different", dates)
	}
	if before := e.digests(today); len(before.Digests) != 2 || before.Digests[0].Date != missed {
		t.Errorf("before today = %+v, want the missed day first", before.Digests)
	}
}
