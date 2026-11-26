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
}

// New returns a Watcher with nothing watched and not running.
func New(d Deps) *Watcher {
	if d.Clock == nil {
		d.Clock = realClock{}
	}
	return &Watcher{table: d.Table, signal: d.Signal, sink: d.Sink, clock: d.Clock, watches: map[int]*watch{}}
}

// watch is one watched session and the process it is ending.
type watch struct {
	ports.MemoryWatch
	// ending is the process being ended, nil when none.
	ending *ending
}

// ending is a process that has been sent SIGTERM and is not gone yet.
type ending struct {
	pid      int
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
	w.mu.Lock()
	id := w.next
	w.next++
	w.watches[id] = &watch{MemoryWatch: m}
	w.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			delete(w.watches, id)
			w.mu.Unlock()
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
	w.cancel, w.done = cancel, make(chan struct{})
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
	table, err := w.table.Read(context.Background())
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
	t, ok := treeOf(table, wt.MemoryWatch)
	if !ok {
		return
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
		if !w.settle(wt, table, now) {
			return // still ending: end nobody else yet
		}
	}
	victim, ok := t.victim(wt.BudgetBytes)
	if !ok {
		if t.rss > wt.BudgetBytes {
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
		pid: victim.PID, command: command(victim), rss: victim.RSSBytes,
		treeRSS: t.rss, budget: wt.BudgetBytes, termAt: now,
	}
}

// settle looks at the process being ended: gone, it is recorded and the watch
// is free again (true); still there past the grace, it gets SIGKILL, once.
func (w *Watcher) settle(wt *watch, table []ports.ProcessInfo, now time.Time) (free bool) {
	e := wt.ending
	present := false
	for _, p := range table {
		if p.PID == e.pid {
			present = true
			break
		}
	}
	if !present {
		if !e.killed {
			w.killed(wt, e, now, syscall.SIGTERM)
		}
		wt.ending = nil
		return true
	}
	if !e.killed && now.Sub(e.termAt) >= Grace {
		if err := w.signal.Signal(e.pid, syscall.SIGKILL); err != nil {
			slog.Warn("memory watch: SIGKILL failed", "session", wt.SessionID, "pid", e.pid, "err", err)
			return false
		}
		e.killed, e.killedAt = true, now
		w.killed(wt, e, now, syscall.SIGKILL)
	}
	return false
}

func (w *Watcher) killed(wt *watch, e *ending, now time.Time, sig syscall.Signal) {
	if w.sink == nil {
		return
	}
	name := "SIGTERM"
	if sig == syscall.SIGKILL {
		name = "SIGKILL"
	}
	w.sink.Killed(domain.MemoryKill{
		Kind: domain.MemoryKilledKind, SessionID: wt.SessionID, TaskID: wt.TaskID, KilledAt: now,
		PID: e.pid, Command: e.command, RSSBytes: e.rss, TreeRSS: e.treeRSS, Budget: e.budget, Signal: name,
	})
}

func (w *Watcher) sampled(s domain.MemorySample) {
	if w.sink != nil {
		w.sink.Sampled(s)
	}
}
