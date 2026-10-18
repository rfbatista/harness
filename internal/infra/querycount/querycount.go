// Package querycount counts the database calls made for one unit of work,
// such as an HTTP request, so a slow request can say how many it made and a
// test can hold a page to a query budget.
//
// A call is attributed to a tracker in one of two ways. A call whose context
// carries the tracker's counter is counted on whatever goroutine makes it.
// A call made without such a context (repositories off the budgeted pages
// take none yet) is counted, by Track alone, when it runs on the goroutine
// that started tracking; on any other goroutine it is not counted.
// TrackContext counts by the context alone.
package querycount

import (
	"bytes"
	"context"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

// Counter is the number of database calls a tracker has counted.
type Counter struct{ n atomic.Int64 }

// Count is the number of calls counted so far.
func (c *Counter) Count() int64 { return c.n.Load() }

type counterKey struct{}

// FromContext is the counter ctx carries; nil when nothing tracks ctx.
func FromContext(ctx context.Context) *Counter {
	c, _ := ctx.Value(counterKey{}).(*Counter)
	return c
}

var (
	// byGoroutine maps a goroutine id to the counter tracking it.
	byGoroutine sync.Map
	// tracking is how many goroutines are tracked, so an untracked call
	// skips looking up its goroutine while nothing is tracked.
	tracking atomic.Int64
)

// Track starts counting the database calls made for ctx: those whose
// context carries the returned context's counter, and those made without
// one on the calling goroutine until stop. A tracker started inside another
// on the same goroutine takes over that goroutine's calls until it stops.
func Track(ctx context.Context) (context.Context, *Counter, func()) {
	c := &Counter{}
	return context.WithValue(ctx, counterKey{}, c), c, bind(c)
}

// TrackContext starts counting the database calls whose context carries the
// returned context's counter, and no other: unlike Track, a call made
// without that context is not counted, on any goroutine. A test holding a
// path to a query budget tracks with it, so the count it holds rests on the
// context alone.
func TrackContext(ctx context.Context) (context.Context, *Counter) {
	c := &Counter{}
	return context.WithValue(ctx, counterKey{}, c), c
}

// bind counts the calling goroutine's calls made without a context for c,
// until the returned func; the tracker it replaced counts them again after.
func bind(c *Counter) func() {
	id := goroutineID()
	prev, hadPrev := byGoroutine.Swap(id, c)
	tracking.Add(1)
	return sync.OnceFunc(func() {
		if hadPrev {
			byGoroutine.Store(id, prev)
		} else {
			byGoroutine.Delete(id)
		}
		tracking.Add(-1)
	})
}

// Add counts one database call made under ctx: to the counter ctx carries,
// else to the tracker of the calling goroutine, else nowhere.
func Add(ctx context.Context) {
	if c := FromContext(ctx); c != nil {
		c.n.Add(1)
		return
	}
	if tracking.Load() == 0 {
		return
	}
	if c, ok := byGoroutine.Load(goroutineID()); ok {
		c.(*Counter).n.Add(1)
	}
}

// goroutineID is the calling goroutine's id, read from the first line of its
// stack trace ("goroutine 42 [running]:"). The runtime never reuses an id.
func goroutineID() uint64 {
	var buf [64]byte
	line := buf[:runtime.Stack(buf[:], false)]
	line = bytes.TrimPrefix(line, []byte("goroutine "))
	if i := bytes.IndexByte(line, ' '); i >= 0 {
		line = line[:i]
	}
	id, _ := strconv.ParseUint(string(line), 10, 64)
	return id
}
