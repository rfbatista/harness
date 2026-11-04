// Package pagetime measures one project page request: its page server time
// and where it went. A request's context carries a Timing; the database and
// git calls made under that context add their time to it, so a page can say
// how long it took and why, in its Server-Timing header and its log line.
package pagetime

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Timing is one request's measurements. It is safe for the goroutines that
// make the request's parallel reads. Their times add up, so db and git can
// exceed the wall-clock total.
type Timing struct {
	start    time.Time
	db       atomic.Int64
	git      atomic.Int64
	gitCalls atomic.Int64
	frame    atomic.Int64
	catalog  atomic.Int64
	reads    atomic.Int64
}

type key struct{}

// Start begins timing a request: the Timing ctx carries from now on.
func Start(ctx context.Context) (context.Context, *Timing) {
	t := &Timing{start: time.Now()}
	return context.WithValue(ctx, key{}, t), t
}

// From is the Timing ctx carries; nil when nothing times ctx.
func From(ctx context.Context) *Timing {
	t, _ := ctx.Value(key{}).(*Timing)
	return t
}

// AddDB counts a database call that took d under ctx.
func AddDB(ctx context.Context, d time.Duration) {
	if t := From(ctx); t != nil {
		t.db.Add(int64(d))
	}
}

// AddGit counts a git process that took d under ctx.
func AddGit(ctx context.Context, d time.Duration) {
	if t := From(ctx); t != nil {
		t.git.Add(int64(d))
		t.gitCalls.Add(1)
	}
}

// AddFrame counts d as time spent building the shared project frame.
func AddFrame(ctx context.Context, d time.Duration) {
	if t := From(ctx); t != nil {
		t.frame.Add(int64(d))
	}
}

var (
	// byGoroutine maps a goroutine id to the Timing bound to it.
	byGoroutine sync.Map
	// bound is how many goroutines are bound, so a read made while no page
	// is timed skips looking up its goroutine.
	bound atomic.Int64
)

// Bind makes the calling goroutine's catalog reads count toward t until the
// returned func is called. The agent catalog is read without a context, so
// the goroutine is what ties a read to its request: a read made on another
// goroutine is not counted.
func (t *Timing) Bind() (unbind func()) {
	id := goroutineID()
	byGoroutine.Store(id, t)
	bound.Add(1)
	return sync.OnceFunc(func() {
		byGoroutine.Delete(id)
		bound.Add(-1)
	})
}

// AddCatalogRead counts one read of the agent catalog that took d, for the
// page the calling goroutine serves, if any.
func AddCatalogRead(d time.Duration) {
	if bound.Load() == 0 {
		return
	}
	if t, ok := byGoroutine.Load(goroutineID()); ok {
		t.(*Timing).catalog.Add(int64(d))
		t.(*Timing).reads.Add(1)
	}
}

// goroutineID is the calling goroutine's id, read from the first line of its
// stack trace ("goroutine 42 [running]:").
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

// Report is a finished request's numbers.
type Report struct {
	Total, DB, Git, Render, Frame, Catalog time.Duration
	GitCalls, CatalogReads                 int64
}

// Report is the numbers so far, with render as the time the page took to
// render its template.
func (t *Timing) Report(render time.Duration) Report {
	return Report{
		Total: time.Since(t.start), DB: time.Duration(t.db.Load()), Git: time.Duration(t.git.Load()),
		GitCalls: t.gitCalls.Load(), Render: render, Frame: time.Duration(t.frame.Load()),
		Catalog: time.Duration(t.catalog.Load()), CatalogReads: t.reads.Load(),
	}
}

// Header is r as a Server-Timing header value. The names are a contract:
// the page benchmark and the budget check read them.
func (r Report) Header() string {
	ms := func(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond)) }
	var b strings.Builder
	fmt.Fprintf(&b, "total;dur=%s, db;dur=%s, git;desc=\"%d calls\";dur=%s, render;dur=%s, frame;dur=%s, catalog;desc=\"%d reads\";dur=%s",
		ms(r.Total), ms(r.DB), r.GitCalls, ms(r.Git), ms(r.Render), ms(r.Frame), r.CatalogReads, ms(r.Catalog))
	return b.String()
}
