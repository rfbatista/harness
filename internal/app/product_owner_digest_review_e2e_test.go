package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The daily digest's edge cases (ADR 0113), found in review: a failed digest
// run, a shutdown in the middle of one, a task that moved more than once, a
// digest time moved earlier, a product owner off at the time, and paging.

// failingHeadlessClaude starts like claude and fails on its first message,
// with no answer and no result.
const failingHeadlessClaude = `#!/bin/sh
dir=$(dirname "$0")
echo "$*" > "$dir/launch.$$"
printf '%s\n' '{"type":"system","subtype":"init","session_id":"fake"}'
read -r line
echo "claude failed" >&2
exit 1
`

// swapClaude replaces the stand-in claude with script.
func (e *poE2E) swapClaude(script string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.fakeDir, "claude"), []byte(script), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

func (e *poE2E) inbox() []*domain.Notification {
	e.t.Helper()
	page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{})
	if err != nil {
		e.t.Fatal(err)
	}
	return page.Notifications
}

func TestAFailedDigestRunIsRetriedLaterAndTheDayIsNotLost(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Moved before a failed run", domain.TicketStatusReview, clk.at(1, 6, 0))
	e.swapClaude(failingHeadlessClaude)

	clk.set(clk.at(1, 9, 0))
	eventuallyPO(t, "the failed digest run", func() string {
		if len(e.launches()) == 0 {
			return "claude was not launched"
		}
		return ""
	})
	settle()
	if n := len(e.digests("").Digests); n != 0 {
		t.Fatalf("%d digests after a failed run, want none", n)
	}
	// Within the retry wait, no other run starts.
	if n := len(e.launches()); n != 1 {
		t.Errorf("claude was launched %d times right after a failed run, want once", n)
	}
	for _, n := range e.inbox() {
		t.Errorf("a failed digest run posted an inbox item: %s %q", n.Kind, n.Title)
	}

	// Later the same day, the run is retried and the day is recorded.
	e.swapClaude(fakeHeadlessClaude)
	clk.set(clk.at(0, 9, 11))
	d := e.waitDigests(1).Digests[0]
	if d.Date != clk.Now().Format("2006-01-02") {
		t.Errorf("date = %s, want the day of the retry", d.Date)
	}
	for _, it := range d.Items {
		if it.Kind == domain.DigestItemWaitingOnPerson || it.Kind == domain.DigestItemSessionFailed {
			t.Errorf("the failed digest run shows in the digest: %+v", it)
		}
	}
	if len(d.Items) != 1 || d.Items[0].Title != "Moved before a failed run" {
		t.Errorf("items = %+v, want the move", d.Items)
	}
}

func TestAShutdownDuringADigestRunEndsItAndTheDayIsRecordedOnceAfterBoot(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Moved before the shutdown", domain.TicketStatusReview, clk.at(1, 6, 0))
	e.hold()
	clk.set(clk.at(1, 9, 0))
	eventuallyPO(t, "the digest run starts", func() string {
		if len(e.launches()) == 0 {
			return "claude was not launched"
		}
		return ""
	})

	e.feed.stop()
	e.srv.Close()
	start := time.Now()
	if err := e.app.Stop(e.ctx); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("the server stopped %s after a digest run began", took)
	if took > 10*time.Second {
		t.Errorf("the server took %s to stop during a digest run", took)
	}
	e.release()
	if n := len(e.launches()); n != 1 {
		t.Fatalf("claude was launched %d times before the shutdown", n)
	}

	e.boot()
	e.serve()
	d := e.waitDigests(1).Digests[0]
	if len(d.Items) != 1 || d.Items[0].Title != "Moved before the shutdown" {
		t.Errorf("items = %+v, want the move", d.Items)
	}
	settle()
	if n := len(e.digests("").Digests); n != 1 {
		t.Errorf("%d digests, want one", n)
	}
	for _, n := range e.inbox() {
		t.Errorf("the cut digest run posted an inbox item: %s %q", n.Kind, n.Title)
	}
}

func TestATaskThatMovedTwiceInOnePeriodIsReportedFromWhereItWasToWhereItIs(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	there := e.moveTask("Moved twice", domain.TicketStatusReview, clk.at(0, 21, 0))
	back := e.moveTask("Moved and back", domain.TicketStatusReview, clk.at(0, 21, 0))
	// The first period ends with both tasks in review; nothing moves after.
	clk.set(clk.at(1, 8, 31))
	e.waitDigests(1)
	launches := len(e.launches())

	// A task that moves and comes back is no change: a day with only that is
	// quiet.
	e.moveExisting(back, domain.TicketStatusInProgress, clk.at(1, 6, 0))
	e.moveExisting(back, domain.TicketStatusReview, clk.at(1, 7, 0))
	clk.set(clk.at(1, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 1 || len(e.launches()) != launches {
		t.Fatalf("%d digests and %d more runs for a task that came back", n, len(e.launches())-launches)
	}

	// A task that moves twice is one item, from the first status to the last.
	e.moveExisting(there, domain.TicketStatusInProgress, clk.at(1, 6, 0))
	e.moveExisting(there, domain.TicketStatusDone, clk.at(1, 7, 0))
	e.moveExisting(back, domain.TicketStatusInProgress, clk.at(1, 6, 30))
	e.moveExisting(back, domain.TicketStatusReview, clk.at(1, 7, 30))
	clk.set(clk.at(1, 9, 0))
	d := e.waitDigests(2).Digests[0]
	if len(d.Items) != 1 || d.Items[0].Title != "Moved twice" || d.Items[0].From != "review" || d.Items[0].To != "done" {
		t.Errorf("items = %+v, want one item from review to done", d.Items)
	}
}

func TestADigestTimeMovedEarlierAfterTheDayWasDigestedWaitsForTheNextDay(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Day one", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 8, 31))
	e.waitDigests(1)

	e.moveTask("Later on day one", domain.TicketStatusReview, clk.at(0, 8, 40))
	clk.set(clk.at(0, 8, 45))
	e.setDigestTime("07:00")
	settle()
	if n := len(e.digests("").Digests); n != 1 {
		t.Fatalf("%d digests after the time moved earlier the same day, want 1", n)
	}
	clk.set(clk.at(1, 6, 59))
	settle()
	if n := len(e.digests("").Digests); n != 1 {
		t.Fatalf("%d digests before the new time, want 1", n)
	}
	clk.set(clk.at(0, 7, 0))
	if d := e.waitDigests(2).Digests[0]; len(d.Items) != 1 || d.Items[0].Title != "Later on day one" {
		t.Errorf("items = %+v, want the later change", d.Items)
	}
}

func TestAProductOwnerOffAtTheTimeRecordsOneDigestForTheWholeGapWhenSwitchedOn(t *testing.T) {
	e, clk := newDigestE2E(t)
	e.setDigestTime("08:30")
	e.moveTask("Day one", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 8, 0))
	e.setEnabled(false)
	clk.set(clk.at(0, 9, 0))
	settle()
	e.moveTask("Day two", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 9, 0))
	settle()
	if n := len(e.digests("").Digests); n != 0 || len(e.launches()) != 0 {
		t.Fatalf("%d digests and %d runs while off over the time", n, len(e.launches()))
	}

	e.setEnabled(true)
	d := e.waitDigests(1).Digests[0]
	if len(d.Items) != 2 {
		t.Errorf("items = %+v, want both days' moves", d.Items)
	}
	settle()
	if n := len(e.launches()); n != 1 {
		t.Errorf("claude ran %d times, want once", n)
	}
}

func TestDigestsBeforeADayWithNoDigestPageFromTheOneBeforeIt(t *testing.T) {
	e, _ := newDigestE2E(t)
	e.seedDigest(e.project, "2030-01-07", "Tuesday.")
	e.seedDigest(e.project, "2030-01-10", "Friday.")
	page := e.digests("2030-01-09")
	if len(page.Digests) != 1 || page.Digests[0].Date != "2030-01-07" || page.HasEarlier {
		t.Errorf("before a quiet day = %+v, want Tuesday alone", page)
	}
	if page := e.digests("2030-01-07"); len(page.Digests) != 0 || page.HasEarlier {
		t.Errorf("before the first digest = %+v, want none", page)
	}
}
