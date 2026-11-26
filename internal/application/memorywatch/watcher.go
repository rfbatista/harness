// Package memorywatch is the vigia de memória (ADR 0005): it samples the
// árvore de processos of each watched session once a second and, when a tree
// passes its orçamento, ends the heaviest eligible process: SIGTERM, then
// SIGKILL after 5 s. It never ends a session, its agent CLI, login shell or
// sandbox wrapper. It reads the process table through a port, once per sample
// for every session together, and tells a sink what it sees.
package memorywatch

import (
	"context"
	"log/slog"
	"sync"
	"syscall"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.MemoryWatcher = (*Watcher)(nil)

// Interval is how often a tree is sampled; Grace is how long an ended
// process has to exit before SIGKILL.
const (
	Interval = time.Second
	Grace    = 5 * time.Second
	// readTimeout bounds one read of the process table.
	readTimeout = 2 * time.Second
)

// Ticker is a clock's ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Clock is time as the vigia sees it, so tests can drive it.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

// Deps are the Watcher's collaborators. Clock defaults to the real one; Sink
// may be nil.
type Deps struct {
	Table  ports.ProcessTable
	Signal ports.ProcessSignaler
	Sink   ports.MemorySink
	Clock  Clock
}

// Watcher is the vigia de memória.
type Watcher struct {
	table  ports.ProcessTable
	signal ports.ProcessSignaler
	sink   ports.MemorySink
	clock  Clock

	mu      sync.Mutex
	watches map[int]*watch
	next    int

	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	// ctx ends when Close is called; table reads run under it.
	ctx context.Context
}

// New returns a Watcher with nothing watched and not running.
func New(d Deps) *Watcher {
	if d.Clock == nil {
		d.Clock = realClock{}
	}
	return &Watcher{table: d.Table, signal: d.Signal, sink: d.Sink, clock: d.Clock, watches: map[int]*watch{}, ctx: context.Background()}
}

// watch is one watched session and the process it is ending.
type watch struct {
	ports.MemoryWatch
	// mu serialises judging the session with stopping it, so the empty last
	// sample is the last thing it publishes and nothing is signalled after.
	mu      sync.Mutex
	stopped bool
	// ending is the process being ended, nil when none.
	ending *ending
	// abandoned are processes that outlived SIGKILL and the bound after it,
	// keyed by pid with their start time: they are never chosen again.
	abandoned map[int]string
	// seen are the processes seen in the tree, by pid and start time.
	seen     map[int]string
	lastWarn time.Time
}

// warnEvery is the least time between two warnings about the same session.
const warnEvery = time.Minute

// giveUpAfter is how long after SIGKILL a process may stay in the table
// before the vigia stops waiting for it (a zombie, a process stuck in I/O).
const giveUpAfter = Grace

// ending is a process that has been sent SIGTERM and is not gone yet.
type ending struct {
	pid      int
	started  string
	name     string
	command  string
	rss      int64
	treeRSS  int64
	budget   int64
	termAt   time.Time
	killed   bool
	killedAt time.Time
}

// Watch starts watching m's session and answers what stops it. Stopping
// publishes one last, empty sample so a consumer can clear what it shows; a
// second stop changes nothing.
func (w *Watcher) Watch(m ports.MemoryWatch) (stop func()) {
	wt := &watch{MemoryWatch: m, abandoned: map[int]string{}, seen: map[int]string{}}
	w.mu.Lock()
	id := w.next
	w.next++
	w.watches[id] = wt
	w.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			delete(w.watches, id)
			w.mu.Unlock()
			wt.mu.Lock()
			defer wt.mu.Unlock()
			wt.stopped = true
			w.sampled(domain.MemorySample{
				Kind: domain.MemorySampleKind, SessionID: m.SessionID, TaskID: m.TaskID,
				SampledAt: w.clock.Now(), Budget: m.BudgetBytes,
			})
		})
	}
}

// Start samples every Interval until Close. Starting twice changes nothing.
func (w *Watcher) Start() {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	if w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel, w.done, w.ctx = cancel, make(chan struct{}), ctx
	go func() {
		defer close(w.done)
		tick := w.clock.NewTicker(Interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C():
				w.Poll(now)
			}
		}
	}()
}

// Close stops sampling and waits for the sample under way. The sessions still
// watched stay so: their stop functions still publish the last sample.
func (w *Watcher) Close() {
	w.runMu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel = nil
	w.runMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// Poll samples the table once, at now, and handles every watched session by
// it.
func (w *Watcher) Poll(now time.Time) {
	w.mu.Lock()
	watches := make([]*watch, 0, len(w.watches))
	for _, wt := range w.watches {
		watches = append(watches, wt)
	}
	w.mu.Unlock()
	if len(watches) == 0 {
		return
	}
	w.runMu.Lock()
	run := w.ctx
	w.runMu.Unlock()
	ctx, cancel := context.WithTimeout(run, readTimeout)
	defer cancel()
	table, err := w.table.Read(ctx)
	if err != nil {
		slog.Warn("memory watch: process table not read", "err", err)
		return
	}
	for _, wt := range watches {
		w.handle(wt, table, now)
	}
}

// handle is one session's sample: publish it, settle the process being ended,
// and end the next one when the tree is still over its orçamento.
func (w *Watcher) handle(wt *watch, table []ports.ProcessInfo, now time.Time) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.stopped {
		return
	}
	t, ok := treeOf(table, wt.MemoryWatch, wt.seen)
	if !ok {
		// The agent has left the table: a process being ended is still
		// settled, so a confirmed kill is never lost.
		if wt.ending != nil {
			w.settle(wt, t, table, now)
		}
		return
	}
	wt.seen = map[int]string{}
	present := map[int]string{}
	for _, p := range table {
		present[p.PID] = p.Started
	}
	for _, p := range t.members {
		wt.seen[p.PID] = p.Started
	}
	for pid, started := range wt.abandoned {
		if present[pid] != started {
			delete(wt.abandoned, pid) // it is gone: nothing left to give up on
		}
	}
	sample := domain.MemorySample{
		Kind: domain.MemorySampleKind, SessionID: wt.SessionID, TaskID: wt.TaskID,
		SampledAt: now, RSSBytes: t.rss, Budget: wt.BudgetBytes, Processes: len(t.members),
	}
	if p, ok := t.heaviest(); ok {
		sample.Heaviest = &domain.MemoryProcess{PID: p.PID, Command: command(p), RSSBytes: p.RSSBytes}
	}
	w.sampled(sample)

	if wt.ending != nil {
		if !w.settle(wt, t, table, now) {
			return // still ending: end nobody else yet
		}
	}
	victim, ok := t.victim(wt.BudgetBytes, func(p ports.ProcessInfo) bool {
		s, gone := wt.abandoned[p.PID]
		return gone && s == p.Started
	})
	if !ok {
		if t.rss > wt.BudgetBytes && now.Sub(wt.lastWarn) >= warnEvery {
			wt.lastWarn = now
			slog.Warn("memory watch: tree over its orçamento with nothing eligible to end",
				"session", wt.SessionID, "rss", t.rss, "budget", wt.BudgetBytes, "agent_rss", t.agentRSS)
		}
		return
	}
	if err := w.signal.Signal(victim.PID, syscall.SIGTERM); err != nil {
		slog.Warn("memory watch: SIGTERM failed", "session", wt.SessionID, "pid", victim.PID, "err", err)
		return
	}
	wt.ending = &ending{
		pid: victim.PID, started: victim.Started, name: victim.Name, command: command(victim), rss: victim.RSSBytes,
		treeRSS: t.rss, budget: wt.BudgetBytes, termAt: now,
	}
	if e, ok := w.sink.(ports.MemoryEnding); ok {
		e.Ending(w.killRecord(wt, wt.ending, now, syscall.SIGTERM))
	}
}

// stillThere is whether the process being ended is still in the table as the
// same process: a pid and its start time identify one, so a pid reused by
// another process is not it, and a victim that execs under another name is.
func stillThere(e *ending, table []ports.ProcessInfo) bool {
	for _, p := range table {
		if p.PID == e.pid {
			return p.Started == e.started
		}
	}
	return false
}

// settle looks at the process being ended: gone, it is recorded and the watch
// is free again (true); still there past the grace, it gets SIGKILL, once; and
// still there a grace after that, it is given up on and the rest of the tree
// is watched again.
func (w *Watcher) settle(wt *watch, _ tree, table []ports.ProcessInfo, now time.Time) (free bool) {
	e := wt.ending
	if !stillThere(e, table) {
		if !e.killed {
			w.killed(wt, e, now, syscall.SIGTERM)
		}
		wt.ending = nil
		return true
	}
	if e.killed {
		if now.Sub(e.killedAt) >= giveUpAfter {
			slog.Warn("memory watch: a process outlived SIGKILL; no longer waiting for it",
				"session", wt.SessionID, "pid", e.pid, "command", e.command)
			wt.abandoned[e.pid] = e.started
			wt.ending = nil
			return true
		}
		return false
	}
	if now.Sub(e.termAt) >= Grace {
		if err := w.signal.Signal(e.pid, syscall.SIGKILL); err != nil {
			slog.Warn("memory watch: SIGKILL failed", "session", wt.SessionID, "pid", e.pid, "err", err)
			return false
		}
		e.killed, e.killedAt = true, now
		w.killed(wt, e, now, syscall.SIGKILL)
	}
	return false
}

func (w *Watcher) killRecord(wt *watch, e *ending, now time.Time, sig syscall.Signal) domain.MemoryKill {
	name := "SIGTERM"
	if sig == syscall.SIGKILL {
		name = "SIGKILL"
	}
	return domain.MemoryKill{
		Kind: domain.MemoryKilledKind, SessionID: wt.SessionID, TaskID: wt.TaskID, KilledAt: now,
		PID: e.pid, Command: e.command, RSSBytes: e.rss, TreeRSS: e.treeRSS, Budget: e.budget, Signal: name,
	}
}

func (w *Watcher) killed(wt *watch, e *ending, now time.Time, sig syscall.Signal) {
	if w.sink == nil {
		return
	}
	w.sink.Killed(w.killRecord(wt, e, now, sig))
}

func (w *Watcher) sampled(s domain.MemorySample) {
	if w.sink != nil {
		w.sink.Sampled(s)
	}
}
