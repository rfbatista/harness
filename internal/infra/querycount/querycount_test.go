package querycount

import (
	"context"
	"sync"
	"testing"
)

// A call whose context carries the counter is counted, on any goroutine.
func TestACallMadeWithTheTrackedContextIsCounted(t *testing.T) {
	ctx, c, stop := Track(context.Background())
	defer stop()

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Add(ctx)
		}()
	}
	wg.Wait()

	if got := c.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
}

// A call made without the request's context, on the goroutine that tracks,
// is counted: repositories that take no context still count.
func TestACallWithoutAContextOnTheTrackingGoroutineIsCounted(t *testing.T) {
	_, c, stop := Track(context.Background())
	defer stop()

	Add(context.Background())
	Add(context.Background())

	if got := c.Count(); got != 2 {
		t.Errorf("Count() = %d, want 2", got)
	}
}

// A call carrying the counter on the tracking goroutine counts once.
func TestACallIsCountedOnce(t *testing.T) {
	ctx, c, stop := Track(context.Background())
	defer stop()

	Add(ctx)

	if got := c.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}
}

// Concurrent requests each count only their own calls.
func TestConcurrentTrackersCountOnlyTheirOwnCalls(t *testing.T) {
	var wg sync.WaitGroup
	counts := make([]int64, 8)
	for i := range counts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, c, stop := Track(context.Background())
			defer stop()
			for range i * 10 {
				Add(context.Background())
			}
			counts[i] = c.Count()
		}()
	}
	wg.Wait()

	for i, got := range counts {
		if got != int64(i*10) {
			t.Errorf("tracker %d counted %d, want %d", i, got, i*10)
		}
	}
}

// Once stopped, a tracker counts no more calls made without its context, and
// an untracked call counts nowhere.
func TestStopEndsCountingOnTheGoroutine(t *testing.T) {
	_, c, stop := Track(context.Background())
	Add(context.Background())
	stop()
	Add(context.Background())

	if got := c.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}
}

// A tracker started inside another counts the calls made until it stops; the
// outer one counts again after.
func TestANestedTrackerCountsUntilItStops(t *testing.T) {
	_, outer, stopOuter := Track(context.Background())
	defer stopOuter()
	_, inner, stopInner := Track(context.Background())
	Add(context.Background())
	stopInner()
	Add(context.Background())

	if inner.Count() != 1 || outer.Count() != 1 {
		t.Errorf("inner = %d, outer = %d, want 1 and 1", inner.Count(), outer.Count())
	}
}

// Counting a call nothing tracks does nothing.
func TestAnUntrackedCallIsIgnored(t *testing.T) {
	Add(context.Background())
	if got := FromContext(context.Background()); got != nil {
		t.Errorf("FromContext(untracked) = %v, want nil", got)
	}
}
