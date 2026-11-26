package memorywatch_test

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/ports"
)

// Tester additions: rules of ADR 0005 the first suite leaves open.

// A sandbox wrapper that repeats the agent's id in its own arguments, and a
// heavy login shell above the CLI: neither is ever ended.
func TestAWrapperCarryingTheAgentsIDIsNeverEnded(t *testing.T) {
	w := newWorld()
	rows := ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 600 * mb, Name: "node", Arg: "jest"})
	rows[1].Session = claudeID // the wrapper repeats --session-id
	rows[1].RSSBytes, rows[2].RSSBytes = 3*gb, 2*gb
	w.table.set(rows...)
	watchPTY(w, 4*gb)
	w.pollFor(8 * time.Second)
	for _, s := range w.sig.sent {
		if s.pid == 100 || s.pid == 101 || s.pid == 102 {
			t.Fatalf("signals = %+v: the wrapper, the login shell or the agent CLI was signalled", w.sig.sent)
		}
	}
	if len(w.sig.sent) == 0 || w.sig.sent[0].pid != 201 {
		t.Errorf("signals = %+v, want the tool process 201 ended", w.sig.sent)
	}
}

// A daemonized process (reparented to launchd, same POSIX session) is eligible,
// and the heaviest one is what gets ended.
func TestADaemonizedProcessCanBeTheOneEnded(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(
		ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 1 * gb, Name: "node", Arg: "jest"},
		ports.ProcessInfo{PID: 202, PPID: 1, SID: 100, RSSBytes: 3500 * mb, Name: "node", Arg: "watcher"},
	)...)
	watchPTY(w, 4*gb)
	w.poll()
	if len(w.sig.sent) != 1 || w.sig.sent[0] != (signal{202, syscall.SIGTERM}) {
		t.Errorf("signals = %+v, want SIGTERM to the daemonized 202", w.sig.sent)
	}
}

// A project service is never ended, however heavy, even when it is what puts
// the rest over: it is not in the tree.
func TestAProjectServiceIsNeitherCountedNorEnded(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(
		ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 100 * mb, Name: "node", Arg: "jest"},
		ports.ProcessInfo{PID: 900, PPID: 50, SID: 900, RSSBytes: 9 * gb, Name: "postgres"},
	)...)
	watchPTY(w, 4*gb)
	w.pollFor(10 * time.Second)
	if len(w.sig.sent) != 0 {
		t.Errorf("signals = %+v, want none: the service is outside the tree", w.sig.sent)
	}
}

// Exactly the orçamento is within it; one byte more is over.
func TestTheOrcamentoIsPassedOnlyByOneByteMore(t *testing.T) {
	for _, c := range []struct {
		name string
		rss  int64
		want int
	}{{"exactly at it", 4*gb - 413*mb, 0}, {"one byte over", 4*gb - 413*mb + 1, 1}} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld()
			w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: c.rss, Name: "node", Arg: "jest"})...)
			watchPTY(w, 4*gb)
			w.poll()
			if len(w.sig.sent) != c.want {
				t.Errorf("signals = %+v, want %d", w.sig.sent, c.want)
			}
		})
	}
}

// SIGKILL comes at 5 s after the SIGTERM, not before, and once.
func TestSIGKILLWaitsTheFullGraceAndIsSentOnce(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 4 * gb, Name: "node", Arg: "jest"})...)
	watchPTY(w, 4*gb)
	w.poll() // SIGTERM at +1 s
	w.pollFor(3 * time.Second)
	w.poll() // 4 s after the SIGTERM: still only the SIGTERM.
	if len(w.sig.sent) != 1 {
		t.Fatalf("signals 4 s after SIGTERM = %+v, want only SIGTERM", w.sig.sent)
	}
	w.poll() // 5 s after
	if len(w.sig.sent) != 2 || w.sig.sent[1].sig != syscall.SIGKILL {
		t.Fatalf("signals 5 s after SIGTERM = %+v, want SIGKILL", w.sig.sent)
	}
	w.pollFor(10 * time.Second)
	if len(w.sig.sent) != 2 || len(w.sink.kills) != 0 {
		t.Errorf("signals = %+v kills = %d, want one SIGKILL and no kill record while it lingers", w.sig.sent, len(w.sink.kills))
	}
}

// After a SIGKILL the process is gone at the next sample: the watch is free and
// ends the next heaviest if the tree is still over.
func TestAfterSIGKILLTheNextSampleEndsTheNextHeaviestIfStillOver(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(
		ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 3 * gb, Name: "node", Arg: "jest"},
		ports.ProcessInfo{PID: 202, PPID: 102, SID: 100, RSSBytes: 2 * gb, Name: "node", Arg: "tsc"},
	)...)
	watchPTY(w, 1*gb) // so 202 alone keeps the tree over
	w.poll()
	w.pollFor(5 * time.Second) // SIGKILL to 201
	w.table.without(201)
	w.poll()
	last := w.sig.sent[len(w.sig.sent)-1]
	if last != (signal{202, syscall.SIGTERM}) {
		t.Errorf("signals = %+v, want SIGTERM to 202 after 201 is gone", w.sig.sent)
	}
	if len(w.sink.kills) != 1 || w.sink.kills[0].PID != 201 || w.sink.kills[0].Signal != "SIGKILL" {
		t.Errorf("kills = %+v, want 201 by SIGKILL", w.sink.kills)
	}
}

// Sessions are judged each by its own orçamento: only the one over loses a process.
func TestSessionsAreJudgedEachByItsOwnOrcamento(t *testing.T) {
	w := newWorld(
		ports.ProcessInfo{PID: 10, PPID: 1, SID: 10, RSSBytes: 5 * mb, Name: "sandbox-exec"},
		ports.ProcessInfo{PID: 11, PPID: 10, SID: 10, RSSBytes: 100 * mb, Name: "claude", Session: "ca"},
		ports.ProcessInfo{PID: 12, PPID: 11, SID: 10, RSSBytes: 3 * gb, Name: "node", Arg: "jest"},
		ports.ProcessInfo{PID: 20, PPID: 1, SID: 20, RSSBytes: 5 * mb, Name: "sandbox-exec"},
		ports.ProcessInfo{PID: 21, PPID: 20, SID: 20, RSSBytes: 100 * mb, Name: "claude", Session: "cb"},
		ports.ProcessInfo{PID: 22, PPID: 21, SID: 20, RSSBytes: 3 * gb, Name: "node", Arg: "vitest"},
	)
	w.watcher.Watch(ports.MemoryWatch{SessionID: "a", BudgetBytes: 2 * gb, ClaudeID: "ca", SID: 10})
	w.watcher.Watch(ports.MemoryWatch{SessionID: "b", BudgetBytes: 8 * gb, ClaudeID: "cb", SID: 20})
	w.poll()
	if len(w.sig.sent) != 1 || w.sig.sent[0] != (signal{12, syscall.SIGTERM}) {
		t.Errorf("signals = %+v, want SIGTERM to session a's 12 only", w.sig.sent)
	}
}

// A refused SIGTERM records no kill and is tried again at the next sample.
func TestARefusedSIGTERMRecordsNoKillAndIsRetried(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 4 * gb, Name: "node", Arg: "jest"})...)
	failing := &failingSignaler{err: errors.New("operation not permitted")}
	w.watcher = newWatcherWith(w, failing)
	watchPTY(w, 4*gb)
	w.pollFor(3 * time.Second)
	if failing.calls != 3 || len(w.sink.kills) != 0 {
		t.Errorf("SIGTERM attempts = %d kills = %d, want 3 attempts and no kill", failing.calls, len(w.sink.kills))
	}
}

type failingSignaler struct {
	err   error
	calls int
}

func (f *failingSignaler) Signal(int, syscall.Signal) error { f.calls++; return f.err }

// An unreadable process table samples nothing, ends nothing and does not stop the watch.
func TestAnUnreadableProcessTableEndsNothingAndTheWatchGoesOn(t *testing.T) {
	w := newWorld()
	bad := &brokenTable{table: w.table, fail: true}
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 4 * gb, Name: "node", Arg: "jest"})...)
	w.watcher = newWatcherWithTable(w, bad)
	watchPTY(w, 4*gb)
	w.poll()
	if len(w.sig.sent) != 0 || len(w.sink.samples) != 0 {
		t.Fatalf("signals = %+v samples = %d, want nothing while the table is unreadable", w.sig.sent, len(w.sink.samples))
	}
	bad.fail = false
	w.poll()
	if len(w.sig.sent) != 1 {
		t.Errorf("signals = %+v, want the watch to work again once the table reads", w.sig.sent)
	}
}

type brokenTable struct {
	table *table
	fail  bool
}

func (b *brokenTable) Read(ctx context.Context) ([]ports.ProcessInfo, error) {
	if b.fail {
		return nil, errors.New("ps: operation not permitted")
	}
	return b.table.Read(ctx)
}

// Stopping a watch while a process is in its grace sends nothing more and
// does not record a kill the session is no longer there to hear.
func TestStoppingAWatchInTheGraceSendsNoSIGKILL(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 4 * gb, Name: "node", Arg: "jest"})...)
	stop := watchPTY(w, 4*gb)
	w.poll()
	stop()
	w.pollFor(10 * time.Second)
	if len(w.sig.sent) != 1 {
		t.Errorf("signals = %+v, want only the first SIGTERM", w.sig.sent)
	}
}

func newWatcherWith(w *world, s ports.ProcessSignaler) *memorywatch.Watcher {
	return newWatcher(w.table, s, w.sink)
}

func newWatcherWithTable(w *world, tb ports.ProcessTable) *memorywatch.Watcher {
	return newWatcher(tb, w.sig, w.sink)
}

func newWatcher(tb ports.ProcessTable, sg ports.ProcessSignaler, sk ports.MemorySink) *memorywatch.Watcher {
	return memorywatch.New(memorywatch.Deps{Table: tb, Signal: sg, Sink: sk})
}
