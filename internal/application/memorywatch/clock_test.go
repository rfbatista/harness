package memorywatch_test

import (
	"testing"
	"time"

	"operators-mcp/internal/application/memorywatch"
)

// fakeClock fires its ticker when a test says so.
type fakeClock struct {
	t   *testing.T
	now time.Time
	c   chan time.Time
	// done is closed by Stop.
	done chan struct{}
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now, c: make(chan time.Time), done: make(chan struct{})}
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) NewTicker(d time.Duration) memorywatch.Ticker {
	if d != time.Second {
		panic("the vigia samples every second")
	}
	return f
}

func (f *fakeClock) C() <-chan time.Time { return f.c }
func (f *fakeClock) Stop()               { close(f.done) }

// tick delivers n ticks; each send returns only when the watcher took it, and
// the next one only after it finished the previous poll. Close then waits for
// the last poll.
func (f *fakeClock) tick(n int) {
	for range n {
		f.now = f.now.Add(time.Second)
		f.c <- f.now
	}
}

// tickWithoutWaiting offers a tick no one has to take.
func (f *fakeClock) tickWithoutWaiting() {
	select {
	case f.c <- f.now.Add(time.Second):
	case <-time.After(50 * time.Millisecond):
	}
}
