// Package pagetime measures one project page request: its page server time
// and where it went. A request's context carries a Timing; the database and
// git calls made under that context add their time to it, so a page can say
// how long it took and why, in its Server-Timing header and its log line.
package pagetime

import (
	"context"
	"fmt"
	"strings"
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

// Report is a finished request's numbers.
type Report struct {
	Total, DB, Git, Render, Frame time.Duration
	GitCalls                      int64
}

// Report is the numbers so far, with render as the time the page took to
// render its template.
func (t *Timing) Report(render time.Duration) Report {
	return Report{
		Total: time.Since(t.start), DB: time.Duration(t.db.Load()), Git: time.Duration(t.git.Load()),
		GitCalls: t.gitCalls.Load(), Render: render, Frame: time.Duration(t.frame.Load()),
	}
}

// Header is r as a Server-Timing header value. The names are a contract:
// the page benchmark and the budget check read them.
func (r Report) Header() string {
	ms := func(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond)) }
	var b strings.Builder
	fmt.Fprintf(&b, "total;dur=%s, db;dur=%s, git;desc=\"%d calls\";dur=%s, render;dur=%s, frame;dur=%s",
		ms(r.Total), ms(r.DB), r.GitCalls, ms(r.Git), ms(r.Render), ms(r.Frame))
	return b.String()
}
