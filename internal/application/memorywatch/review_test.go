package memorywatch_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Review findings F1–F14 on the vigia.

// endingSink is a sink that also hears of a SIGTERM the moment it is sent.
type endingSink struct {
	*sink
	mu2     sync.Mutex
	endings []domain.MemoryKill
	order   []string
}

func (e *endingSink) Ending(k domain.MemoryKill) {
	e.mu2.Lock()
	defer e.mu2.Unlock()
	e.endings = append(e.endings, k)
	e.order = append(e.order, "ending")
}

func (e *endingSink) Killed(k domain.MemoryKill) {
	e.mu2.Lock()
	e.order = append(e.order, "killed")
	e.mu2.Unlock()
	e.sink.Killed(k)
}

func heavyTool(pid int, rss int64) ports.ProcessInfo {
	return ports.ProcessInfo{PID: pid, PPID: 102, SID: 100, RSSBytes: rss, Name: "node", Arg: "jest", Started: "Thu Nov 26 08:00:00 2026"}
}

// F1: the sink is told when SIGTERM goes out, before the process is seen gone.
func TestTheSinkHearsOfAnEndingTheMomentSIGTERMIsSent(t *testing.T) {
	w := newWorld()
	es := &endingSink{sink: w.sink}
	w.watcher = memorywatch.New(memorywatch.Deps{Table: w.table, Signal: w.sig, Sink: es})
	w.table.set(ptyTree(heavyTool(201, 4*gb))...)
	watchPTY(w, 4*gb)
	w.poll()
	if len(es.endings) != 1 || es.endings[0].PID != 201 || es.endings[0].Signal != "SIGTERM" || es.endings[0].Command != "node jest" || len(w.sink.kills) != 0 {
		t.Fatalf("after SIGTERM: endings %+v, kills %+v; want the ending only", es.endings, w.sink.kills)
	}
	w.table.without(201)
	w.poll()
	if got := strings.Join(es.order, ","); got != "ending,killed" || len(w.sink.kills) != 1 {
		t.Errorf("order = %s, kills = %d", got, len(w.sink.kills))
	}
}

// F2: the agent leaving the table with the ending is no reason to lose the kill.
func TestAConfirmedKillIsRecordedEvenWhenTheAgentHasLeftTheTable(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(heavyTool(201, 4*gb))...)
	watchPTY(w, 4*gb)
	w.poll()
	w.table.without(100, 101, 102, 201) // the stand-in agent exits with its child
	w.poll()
	if len(w.sink.kills) != 1 || w.sink.kills[0].PID != 201 || w.sink.kills[0].Signal != "SIGTERM" {
		t.Errorf("kills = %+v, want the one for 201", w.sink.kills)
	}
}

// slowTable blocks a read until released, to open the window between reading
// the table and judging a session by it.
type slowTable struct {
	inner   *table
	reading chan struct{}
	release chan struct{}
}

func (s *slowTable) Read(ctx context.Context) ([]ports.ProcessInfo, error) {
	rows, _ := s.inner.Read(ctx)
	close(s.reading)
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return rows, nil
}

// F3: once stop has run, the empty sample is the last thing the session hears
// and nothing is signalled for it.
func TestNoSampleAndNoSignalFollowTheFinalEmptySample(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(heavyTool(201, 4*gb))...)
	st := &slowTable{inner: w.table, reading: make(chan struct{}), release: make(chan struct{})}
	w.watcher = memorywatch.New(memorywatch.Deps{Table: st, Signal: w.sig, Sink: w.sink})
	stop := watchPTY(w, 4*gb)

	done := make(chan struct{})
	go func() { w.watcher.Poll(t0.Add(time.Second)); close(done) }()
	<-st.reading
	stop()
	close(st.release)
	<-done

	last := w.sink.last()
	if last.RSSBytes != 0 || last.Processes != 0 {
		t.Errorf("last sample = %+v, want the empty one last", last)
	}
	if len(w.sig.sent) != 0 {
		t.Errorf("signals after stop = %+v, want none", w.sig.sent)
	}
}

// F4: a PID reused within the grace by another process is not signalled.
func TestAReusedPIDIsNotSentSIGKILL(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(heavyTool(201, 4*gb))...)
	watchPTY(w, 4*gb)
	w.poll() // SIGTERM to 201
	other := heavyTool(201, 50*mb)
	other.Started = "Thu Nov 26 09:30:00 2026" // a different process, same number
	w.table.set(ptyTree(other)...)
	w.pollFor(7 * time.Second)
	for _, s := range w.sig.sent {
		if s.sig == syscall.SIGKILL {
			t.Fatalf("signals = %+v: the reused PID got SIGKILL", w.sig.sent)
		}
	}
	if len(w.sink.kills) != 1 || w.sink.kills[0].Signal != "SIGTERM" {
		t.Errorf("kills = %+v, want the original recorded as gone", w.sink.kills)
	}
}

// F5: the agent is the innermost process carrying its id, not the highest PID.
func TestAWrapperWithAHigherPIDThanTheAgentIsStillAnAncestorNeverTheVictim(t *testing.T) {
	w := newWorld()
	rows := []ports.ProcessInfo{
		{PID: 1, PPID: 0, SID: 1, RSSBytes: 10 * mb, Name: "launchd"},
		{PID: 100, PPID: 1, SID: 100, RSSBytes: 5 * mb, Name: "sandbox-exec"},
		{PID: 500, PPID: 100, SID: 100, RSSBytes: 30 * mb, Name: "wrapper", Arg: "--session-id", Session: claudeID},
		{PID: 102, PPID: 500, SID: 100, RSSBytes: 3 * gb, Name: "claude", Arg: "--session-id", Session: claudeID},
		heavyTool(201, 2*gb),
	}
	w.table.set(rows...)
	watchPTY(w, 4*gb)
	w.pollFor(8 * time.Second)
	for _, s := range w.sig.sent {
		if s.pid == 500 || s.pid == 102 || s.pid == 100 {
			t.Fatalf("signals = %+v: the wrapper or the agent CLI was signalled", w.sig.sent)
		}
	}
	if len(w.sig.sent) == 0 || w.sig.sent[0].pid != 201 {
		t.Errorf("signals = %+v, want 201 ended", w.sig.sent)
	}
}

// hangingTable blocks until its context ends, as a hung ps does.
type hangingTable struct{ started chan struct{} }

func (h *hangingTable) Read(ctx context.Context) ([]ports.ProcessInfo, error) {
	close(h.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

// F7: a hung table read never blocks Close.
func TestCloseDoesNotWaitOnAHungProcessTableRead(t *testing.T) {
	clock := newFakeClock(t0)
	h := &hangingTable{started: make(chan struct{})}
	w := memorywatch.New(memorywatch.Deps{Table: h, Signal: &signaler{}, Sink: &sink{}, Clock: clock})
	w.Watch(ports.MemoryWatch{SessionID: "s1", BudgetBytes: 4 * gb, ClaudeID: claudeID})
	w.Start()
	clock.c <- t0.Add(time.Second)
	<-h.started
	closed := make(chan struct{})
	go func() { w.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close is stuck behind a hung ps")
	}
}

// F10: a tree over its orçamento with nothing to end warns once, not every second.
func TestTheNothingEligibleWarningIsNotLoggedEverySecond(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	w := newWorld()
	rows := ptyTree()
	rows[3].RSSBytes = 5 * gb // the agent alone is over
	w.table.set(append(rows, ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 100 * mb, Name: "node"})...)
	watchPTY(w, 4*gb)
	w.pollFor(10 * time.Second)
	if n := strings.Count(buf.String(), "nothing eligible"); n != 1 {
		t.Errorf("warnings = %d in 10 s, want 1", n)
	}
}

// F11: a process that survives SIGKILL is given up on after a bound; the rest
// of the tree is still watched and ended.
func TestAProcessThatSurvivesSIGKILLIsGivenUpOnAndTheRestIsStillWatched(t *testing.T) {
	w := newWorld()
	// 202 is over the orçamento on its own once 201 no longer counts.
	w.table.set(ptyTree(heavyTool(201, 4*gb), heavyTool(202, 3800*mb))...)
	watchPTY(w, 4*gb)
	w.pollFor(14 * time.Second)
	var ended202 bool
	for _, s := range w.sig.sent {
		if s.pid == 202 && s.sig == syscall.SIGTERM {
			ended202 = true
		}
	}
	if !ended202 {
		t.Errorf("signals = %+v, want 202 ended once 201 is given up on", w.sig.sent)
	}
	if n := countSignals(w.sig.sent, 201, syscall.SIGKILL); n != 1 {
		t.Errorf("SIGKILL to 201 sent %d times, want once", n)
	}
}

func countSignals(sent []signal, pid int, sig syscall.Signal) int {
	n := 0
	for _, s := range sent {
		if s.pid == pid && s.sig == sig {
			n++
		}
	}
	return n
}

// F14: a command is cut at a rune boundary.
func TestACommandIsCutAtARuneBoundary(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(ports.ProcessInfo{PID: 201, PPID: 102, SID: 100, RSSBytes: 900 * mb, Name: "node", Arg: strings.Repeat("é", 100)})...)
	watchPTY(w, 4*gb)
	w.poll()
	cmd := w.sink.last().Heaviest.Command
	if !utf8.ValidString(cmd) || len(cmd) > 80 || len(cmd) < 78 {
		t.Errorf("command = %q (%d bytes), want valid UTF-8 of at most 80 bytes", cmd, len(cmd))
	}
}

// N3: a process the vigia has given up on no longer drives further kills. It is
// still reported in the sample's RSS, but the rest of the tree is not ended
// because of it.
func TestAProcessGivenUpOnDoesNotMakeTheVigiaEndTheRestOfTheTree(t *testing.T) {
	w := newWorld()
	stuck := heavyTool(201, 3*gb)
	w.table.set(ptyTree(stuck, heavyTool(202, 800*mb))...)
	watchPTY(w, 4*gb)
	w.pollFor(14 * time.Second) // 201 is ended, killed, then given up on
	n := countSignals(w.sig.sent, 202, syscall.SIGTERM)
	if n != 0 {
		t.Errorf("signals = %+v: 202 was ended because of the process that cannot be", w.sig.sent)
	}
	if got := w.sink.last(); got.RSSBytes < 3*gb {
		t.Errorf("the sample's RSS = %d MB, want the stuck process still counted in it", got.RSSBytes/mb)
	}
}

// N4: a pid and its start time identify a process; a victim that execs under
// another name is still the same process and is not recorded as gone.
func TestAVictimThatChangesItsNameIsStillThere(t *testing.T) {
	w := newWorld()
	w.table.set(ptyTree(heavyTool(201, 4*gb))...)
	watchPTY(w, 4*gb)
	w.poll() // SIGTERM to 201
	renamed := heavyTool(201, 4*gb)
	renamed.Name = "node-renamed"
	w.table.set(ptyTree(renamed)...)
	w.poll()
	w.poll()
	if len(w.sink.kills) != 0 {
		t.Errorf("kills = %+v, want none while the process is still there", w.sink.kills)
	}
}

// Q3: a process once seen in the session's tree stays in it after it is
// reparented to launchd, even when ps prints no session for it.
func TestAReparentedProcessStaysInTheTreeWithoutAPOSIXSession(t *testing.T) {
	w := newWorld()
	child := ports.ProcessInfo{PID: 300, PPID: 102, SID: 0, RSSBytes: 3 * gb, Name: "node", Arg: "dev", Started: "Thu Nov 26 08:00:01 2026"}
	rows := ptyTree(child)
	for i := range rows {
		rows[i].SID = 0 // ps prints 0 for every session
	}
	w.table.set(rows...)
	w.watcher.Watch(ports.MemoryWatch{SessionID: "s1", BudgetBytes: 10 * gb, ClaudeID: claudeID})
	w.poll()
	if got := w.sink.last(); got.Processes != 2 {
		t.Fatalf("before the reparent: processes = %d, want 2 (the agent and its child)", got.Processes)
	}
	orphan := child
	orphan.PPID = 1 // its parent shell exited
	next := ptyTree(orphan)
	for i := range next {
		next[i].SID = 0
	}
	w.table.set(next...)
	w.poll()
	if got := w.sink.last(); got.Processes != 2 || got.RSSBytes < 3*gb {
		t.Errorf("after the reparent: %d processes, %d MB, want the orphan still counted", got.Processes, got.RSSBytes/mb)
	}
	// A different process that reuses the pid is not a member.
	other := orphan
	other.Started = "Thu Nov 26 09:30:00 2026"
	again := ptyTree(other)
	for i := range again {
		again[i].SID = 0
	}
	w.table.set(again...)
	w.poll()
	if got := w.sink.last(); got.Processes != 1 {
		t.Errorf("a reused pid is counted: %d processes, want 1", got.Processes)
	}
}

// failSink also hears of a process that could not be ended.
type failSink struct {
	*sink
	failed []domain.MemoryKill
}

func (f *failSink) KillFailed(k domain.MemoryKill) { f.failed = append(f.failed, k) }

// L1: a process that survives SIGKILL is never reported as ended. Once it is
// given up on, the sink hears of the failure, once, and nothing else.
func TestAProcessThatSurvivesSIGKILLIsReportedAsFailedNeverAsEnded(t *testing.T) {
	w := newWorld()
	fs := &failSink{sink: w.sink}
	w.watcher = memorywatch.New(memorywatch.Deps{Table: w.table, Signal: w.sig, Sink: fs})
	w.table.set(ptyTree(heavyTool(201, 4*gb))...)
	watchPTY(w, 4*gb)
	w.pollFor(30 * time.Second)
	if len(w.sink.kills) != 0 {
		t.Errorf("kills = %+v, want none: the process never left", w.sink.kills)
	}
	if len(fs.failed) != 1 || fs.failed[0].PID != 201 || fs.failed[0].Command != "node jest" || fs.failed[0].Signal != "SIGKILL" {
		t.Errorf("failures = %+v, want one for 201 after SIGKILL", fs.failed)
	}
}
