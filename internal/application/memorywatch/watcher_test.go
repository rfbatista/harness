package memorywatch_test

import (
	"context"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const (
	mb = int64(1) << 20
	gb = int64(1) << 30
)

var t0 = time.Date(2026, 11, 25, 22, 40, 0, 0, time.UTC)

// table is a scripted process table; reads counts how often it was read.
type table struct {
	mu    sync.Mutex
	rows  []ports.ProcessInfo
	reads int
}

func (t *table) Read(context.Context) ([]ports.ProcessInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reads++
	return append([]ports.ProcessInfo(nil), t.rows...), nil
}

func (t *table) set(rows ...ports.ProcessInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows = rows
}

// without removes pids, as a process that has exited.
func (t *table) without(pids ...int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var keep []ports.ProcessInfo
	for _, r := range t.rows {
		gone := false
		for _, p := range pids {
			gone = gone || r.PID == p
		}
		if !gone {
			keep = append(keep, r)
		}
	}
	t.rows = keep
}

type signal struct {
	pid int
	sig syscall.Signal
}

type signaler struct{ sent []signal }

func (s *signaler) Signal(pid int, sig syscall.Signal) error {
	s.sent = append(s.sent, signal{pid, sig})
	return nil
}

type sink struct {
	mu      sync.Mutex
	samples []domain.MemorySample
	kills   []domain.MemoryKill
}

func (s *sink) Sampled(m domain.MemorySample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, m)
}

func (s *sink) Killed(k domain.MemoryKill) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kills = append(s.kills, k)
}

func (s *sink) last() domain.MemorySample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.samples[len(s.samples)-1]
}

type world struct {
	table   *table
	sig     *signaler
	sink    *sink
	watcher *memorywatch.Watcher
	now     time.Time
}

func newWorld(rows ...ports.ProcessInfo) *world {
	w := &world{table: &table{rows: rows}, sig: &signaler{}, sink: &sink{}, now: t0}
	w.watcher = memorywatch.New(memorywatch.Deps{Table: w.table, Signal: w.sig, Sink: w.sink})
	return w
}

// poll advances the clock by a second and samples once.
func (w *world) poll() {
	w.now = w.now.Add(time.Second)
	w.watcher.Poll(w.now)
}

func (w *world) pollFor(d time.Duration) {
	for end := w.now.Add(d); w.now.Before(end); {
		w.poll()
	}
}

// The session's PTY is the POSIX session 100: a sandbox wrapper (100) → login
// shell (101) → the agent CLI (102) → its tool processes.
const claudeID = "c-1"

func ptyTree(extra ...ports.ProcessInfo) []ports.ProcessInfo {
	base := []ports.ProcessInfo{
		{PID: 1, PPID: 0, SID: 1, RSSBytes: 10 * mb, Name: "launchd"},
		{PID: 100, PPID: 1, SID: 100, RSSBytes: 5 * mb, Name: "sandbox-exec"},
		{PID: 101, PPID: 100, SID: 100, RSSBytes: 8 * mb, Name: "zsh", Arg: "-l"},
		{PID: 102, PPID: 101, SID: 100, RSSBytes: 400 * mb, Name: "claude", Arg: "--session-id", Session: claudeID},
	}
	return append(base, extra...)
}

func watchPTY(w *world, budget int64) func() {
	return w.watcher.Watch(ports.MemoryWatch{SessionID: "s1", TaskID: "t1", BudgetBytes: budget, ClaudeID: claudeID, SID: 100})
}

// The tree is the agent CLI, its descendants and every process in the PTY's
// POSIX session, reparented ones included; a project service (its own session,
// not descended from the agent) is not part of it.
func TestTheArvoreDeProcessosCountsTheSessionAndItsDescendantsButNotServices(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(
		ports.ProcessInfo{PID: 200, PPID: 102, SID: 100, RSSBytes: 100 * mb, Name: "zsh", Arg: "-c"},
		ports.ProcessInfo{PID: 201, PPID: 200, SID: 100, RSSBytes: 300 * mb, Name: "node", Arg: "jest"},
		// Daemonized: reparented to launchd but still in the PTY's session.
		ports.ProcessInfo{PID: 202, PPID: 1, SID: 100, RSSBytes: 50 * mb, Name: "node", Arg: "watcher"},
		// A project service the server runs: its own session, parent is the server.
		ports.ProcessInfo{PID: 900, PPID: 50, SID: 900, RSSBytes: 2 * gb, Name: "postgres"},
		// A process of another session entirely.
		ports.ProcessInfo{PID: 901, PPID: 1, SID: 901, RSSBytes: 1 * gb, Name: "Safari"},
	)...)
	watchPTY(w, 4*gb)
	w.poll()

	got := w.sink.last()
	if want := (5 + 8 + 400 + 100 + 300 + 50) * mb; got.RSSBytes != want {
		t.Errorf("tree RSS = %d MB, want %d MB", got.RSSBytes/mb, want/mb)
	}
	if got.Processes != 6 {
		t.Errorf("processes = %d, want 6", got.Processes)
	}
	if got.Kind != "session.memory" || got.SessionID != "s1" || got.TaskID != "t1" || got.Budget != 4*gb || !got.SampledAt.Equal(w.now) {
		t.Errorf("sample = %+v", got)
	}
	if got.Heaviest == nil || got.Heaviest.PID != 201 || got.Heaviest.Command != "node jest" || got.Heaviest.RSSBytes != 300*mb {
		t.Errorf("heaviest = %+v, want node jest (201) at 300 MB: the agent CLI is never eligible", got.Heaviest)
	}
}

// A headless session shares the server's POSIX session: only descendants of its agent CLI count.
func TestAHeadlessSessionIsWatchedByDescendancyOnly(t *testing.T) {
	w := newWorld(
		ports.ProcessInfo{PID: 50, PPID: 1, SID: 7, RSSBytes: 900 * mb, Name: "server"},
		ports.ProcessInfo{PID: 60, PPID: 50, SID: 7, RSSBytes: 300 * mb, Name: "claude", Session: claudeID},
		ports.ProcessInfo{PID: 61, PPID: 60, SID: 7, RSSBytes: 200 * mb, Name: "node", Arg: "vitest"},
		ports.ProcessInfo{PID: 62, PPID: 50, SID: 7, RSSBytes: 700 * mb, Name: "postgres"}, // a sibling service, same SID
	)
	w.watcher.Watch(ports.MemoryWatch{SessionID: "s1", TaskID: "t1", BudgetBytes: 4 * gb, ClaudeID: claudeID})
	w.poll()
	if got := w.sink.last(); got.RSSBytes != 500*mb || got.Processes != 2 {
		t.Errorf("headless tree = %d MB over %d processes, want 500 MB over 2", got.RSSBytes/mb, got.Processes)
	}
}

// Over the orçamento, the heaviest eligible process gets SIGTERM; the agent CLI,
// its login shell and the sandbox wrapper are never eligible, however heavy.
func TestOverTheOrcamentoTheHeaviestEligibleProcessGetsSIGTERMAndNeverTheAgent(t *testing.T) {
	w := newWorld()
	rows := ptyTree(
		ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 900 * mb, Name: "node", Arg: "jest"},
		ports.ProcessInfo{PID: 202, PPID: 102, SID: 100, RSSBytes: 600 * mb, Name: "node", Arg: "tsc"},
	)
	// Make the login shell and the wrapper the heaviest of all.
	rows[1].RSSBytes, rows[2].RSSBytes = 3*gb, 2*gb
	w.table.set(rows...)
	watchPTY(w, 4*gb)
	w.poll()

	if len(w.sig.sent) != 1 || w.sig.sent[0] != (signal{201, syscall.SIGTERM}) {
		t.Errorf("signals = %+v, want SIGTERM to 201 only", w.sig.sent)
	}
}

func TestUnderTheOrcamentoNothingIsEnded(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 900 * mb, Name: "node", Arg: "jest"})...)
	watchPTY(w, 4*gb)
	w.pollFor(10 * time.Second)
	if len(w.sig.sent) != 0 || len(w.sink.kills) != 0 {
		t.Errorf("signals = %+v, kills = %+v, want none", w.sig.sent, w.sink.kills)
	}
}

// When the agent CLI alone is over the orçamento, ending its tool processes would not help: nothing is ended.
func TestNothingIsEndedWhenTheAgentCLIAloneIsOverTheOrcamento(t *testing.T) {
	w := newWorld()
	rows := ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 100 * mb, Name: "node", Arg: "jest"})
	rows[3].RSSBytes = 5 * gb
	w.table.set(rows...)
	watchPTY(w, 4*gb)
	w.pollFor(10 * time.Second)
	if len(w.sig.sent) != 0 {
		t.Errorf("signals = %+v, want none", w.sig.sent)
	}
}

// SIGTERM, and the process is gone by the next sample: one kill, signal SIGTERM,
// with the numbers at the moment it was chosen.
func TestAProcessThatExitsAfterSIGTERMIsRecordedWithSIGTERM(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 4 * gb, Name: "node", Arg: "jest"})...)
	watchPTY(w, 4*gb)
	w.poll()
	w.table.without(201)
	w.poll()

	if len(w.sink.kills) != 1 {
		t.Fatalf("kills = %+v, want one", w.sink.kills)
	}
	k := w.sink.kills[0]
	if k.Kind != "session.memory_killed" || k.SessionID != "s1" || k.TaskID != "t1" || k.PID != 201 || k.Command != "node jest" ||
		k.RSSBytes != 4*gb || k.TreeRSS != 4*gb+413*mb || k.Budget != 4*gb || k.Signal != "SIGTERM" {
		t.Errorf("kill = %+v", k)
	}
	if len(w.sig.sent) != 1 {
		t.Errorf("signals = %+v, want only the SIGTERM", w.sig.sent)
	}
}

// A process that outlives the 5 s grace gets SIGKILL, recorded as SIGKILL.
func TestAProcessThatOutlivesTheGraceGetsSIGKILLAfterFiveSeconds(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 4 * gb, Name: "node", Arg: "jest"})...)
	watchPTY(w, 4*gb)
	w.poll() // SIGTERM at t0+1s
	w.pollFor(3 * time.Second)
	if len(w.sig.sent) != 1 {
		t.Fatalf("signals after 3 s = %+v, want only SIGTERM", w.sig.sent)
	}
	w.pollFor(2 * time.Second) // t0+6s: the grace has run out
	if len(w.sig.sent) != 2 || w.sig.sent[1] != (signal{201, syscall.SIGKILL}) {
		t.Fatalf("signals after 5 s = %+v, want SIGTERM then SIGKILL to 201", w.sig.sent)
	}
	if len(w.sink.kills) != 1 || w.sink.kills[0].Signal != "SIGKILL" {
		t.Errorf("kills = %+v, want one, SIGKILL", w.sink.kills)
	}
}

// While a process is in its grace, no second one is ended; once it is gone the
// next sample re-evaluates and ends the next heaviest if the tree is still over.
func TestTheNextSampleReevaluatesOnceTheEndedProcessIsGone(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(
		ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 3 * gb, Name: "node", Arg: "jest"},
		ports.ProcessInfo{PID: 202, PPID: 102, SID: 100, RSSBytes: 2 * gb, Name: "node", Arg: "tsc"},
	)...)
	watchPTY(w, 4*gb)
	w.poll()
	w.poll() // 201 still there: wait for it, end nobody else
	if len(w.sig.sent) != 1 {
		t.Fatalf("signals = %+v, want only the SIGTERM to 201", w.sig.sent)
	}
	w.table.without(201)
	w.poll() // 201 recorded; 5.4 GB → 2.4 GB: under, nothing more
	if len(w.sig.sent) != 1 || len(w.sink.kills) != 1 {
		t.Errorf("signals = %+v, kills = %d, want the tree under after 201", w.sig.sent, len(w.sink.kills))
	}
}

func TestACommandIsTheNameAndItsFirstArgumentAtMostEightyCharacters(t *testing.T) {
	long := strings.Repeat("x", 200)
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 900 * mb, Name: "node", Arg: long})...)
	watchPTY(w, 4*gb)
	w.poll()
	cmd := w.sink.last().Heaviest.Command
	if len(cmd) != 80 || !strings.HasPrefix(cmd, "node xxxx") {
		t.Errorf("command = %q (%d), want 80 characters of the name and first argument", cmd, len(cmd))
	}
}

// One table read per sample serves every watched session.
func TestOneTableReadServesEveryWatchedSession(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree()...)
	for _, id := range []string{"a", "b", "c"} {
		w.watcher.Watch(ports.MemoryWatch{SessionID: id, BudgetBytes: 4 * gb, ClaudeID: claudeID, SID: 100})
	}
	w.poll()
	if w.table.reads != 1 {
		t.Errorf("table reads = %d, want 1", w.table.reads)
	}
	if len(w.sink.samples) != 3 {
		t.Errorf("samples = %d, want one per session", len(w.sink.samples))
	}
}

// Stopping a watch publishes one last empty sample and ends sampling for it.
func TestStoppingAWatchPublishesTheLastEmptySampleAndSamplesNoMore(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree()...)
	stop := watchPTY(w, 4*gb)
	w.poll()
	stop()
	last := w.sink.last()
	if last.RSSBytes != 0 || last.Processes != 0 || last.Heaviest != nil || last.SessionID != "s1" {
		t.Errorf("last sample = %+v, want empty", last)
	}
	n := len(w.sink.samples)
	w.poll()
	stop() // a second stop changes nothing
	if len(w.sink.samples) != n {
		t.Errorf("samples after stop = %d, want %d", len(w.sink.samples), n)
	}
}

// Before the agent CLI shows in the table nothing is counted or ended.
func TestNothingIsCountedBeforeTheAgentCLIAppears(t *testing.T) {
	w := newWorld(ports.ProcessInfo{PID: 100, PPID: 1, SID: 100, RSSBytes: 9 * gb, Name: "sandbox-exec"})
	watchPTY(w, 4*gb)
	w.poll()
	if len(w.sig.sent) != 0 {
		t.Errorf("signals = %+v, want none", w.sig.sent)
	}
}

// Run polls every second on the clock it is given, and Close stops it for good.
func TestRunSamplesEverySecondAndCloseStopsIt(t *testing.T) {
	clock := newFakeClock(t0)
	w := newWorld()
	w.table.set(ptyTree()...)
	w.watcher = memorywatch.New(memorywatch.Deps{Table: w.table, Signal: w.sig, Sink: w.sink, Clock: clock})
	watchPTY(w, 4*gb)
	w.watcher.Start()
	clock.tick(3)
	w.watcher.Close()
	if n := len(w.sink.samples); n != 3 {
		t.Errorf("samples after 3 ticks = %d, want 3", n)
	}
	clock.tickWithoutWaiting()
	if n := len(w.sink.samples); n != 3 {
		t.Errorf("samples after Close = %d, want still 3", n)
	}
}
