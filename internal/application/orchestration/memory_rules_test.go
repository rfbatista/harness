//go:build !windows

package orchestration

import (
	"context"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Tester additions at the orchestration seam: rules of the spec the first
// suite leaves open.

// The server's own NODE_OPTIONS is what the agent would inherit, so it is kept
// and the ceiling added after it (user story 7); an empty one adds no stray space.
func TestTheServersNodeOptionsIsKeptAndAnEmptyOneAddsNoSpace(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "--trace-warnings")
	_, launch := startHeadlessWithBudget(t, 4*gb)
	if got, want := launch.Env["NODE_OPTIONS"], "--trace-warnings --max-old-space-size=1365"; got != want {
		t.Errorf("NODE_OPTIONS = %q, want %q", got, want)
	}
	t.Setenv("NODE_OPTIONS", "--max-old-space-size=900")
	_, launch = startHeadlessWithBudget(t, 4*gb)
	if got, want := launch.Env["NODE_OPTIONS"], "--max-old-space-size=900"; got != want {
		t.Errorf("NODE_OPTIONS = %q, want the server's own ceiling kept: %q", got, want)
	}
	t.Setenv("NODE_OPTIONS", "")
	_, launch = startHeadlessWithBudget(t, 4*gb, "NODE_OPTIONS=")
	if got, want := launch.Env["NODE_OPTIONS"], "--max-old-space-size=1365"; got != want {
		t.Errorf("NODE_OPTIONS = %q, want only the ceiling %q", got, want)
	}
}

// A worker count that does not divide evenly rounds down, with the heap taken
// from the same orçamento: 5 GB → 2 workers, ⌊5120 ÷ 3⌋ MB.
func TestPrevencaoRoundsDownForABudgetThatIsNotAWholeNumberOfWorkers(t *testing.T) {
	_, launch := startHeadlessWithBudget(t, 5*gb)
	if launch.Env["HARNESS_TEST_WORKERS"] != "2" || launch.Env["NODE_OPTIONS"] != "--max-old-space-size=1706" {
		t.Errorf("workers %q, NODE_OPTIONS %q; want 2 and 1706 MB", launch.Env["HARNESS_TEST_WORKERS"], launch.Env["NODE_OPTIONS"])
	}
}

// An override that is not a positive number of bytes is ignored: the next
// level, and in the end the global default, stands (user story 4).
func TestAnInvalidOverrideFallsThroughToTheNextLevel(t *testing.T) {
	for _, bad := range []string{"0", "-5", "abc", ""} {
		t.Run("task value "+strconv.Quote(bad), func(t *testing.T) {
			svc, w := newHeadlessShellService(t)
			savers := withTokenSavers(t, svc)
			setOverride(t, savers, domain.ActivationLevelRepository, "r1", 6*gb)
			if err := savers.activations.Put(domain.Activation{Saver: domain.MemoryBudgetSaver, Owner: domain.ActivationOwner{Level: domain.ActivationLevelTask, ID: w.task.ID}, ProjectID: "p1", Value: domain.ActivationValue(bad)}); err != nil {
				t.Skipf("the store refuses %q already: %v", bad, err)
			}
			sess, _ := startHeadless(t, svc, w.task.ID)
			if sess.MemoryBudgetBytes != 6*gb || sess.MemoryBudgetSource != "repository" {
				t.Errorf("orçamento = %d from %q, want the repository's 6 GB", sess.MemoryBudgetBytes, sess.MemoryBudgetSource)
			}
		})
	}
}

// The source of a task override is reported as "task", a repository's as
// "repository", the default as "global".
func TestTheBudgetSourceNamesTheLevelItCameFrom(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	if sess, _ := startHeadless(t, svc, w.task.ID); sess.MemoryBudgetSource != "global" {
		t.Errorf("source = %q, want global", sess.MemoryBudgetSource)
	}
	setOverride(t, savers, domain.ActivationLevelTask, w.task.ID, 3*gb)
	if sess, _ := startHeadless(t, svc, w.task.ID); sess.MemoryBudgetSource != "task" || sess.MemoryBudgetBytes != 3*gb {
		t.Errorf("source = %q bytes %d, want task 3 GB", sess.MemoryBudgetSource, sess.MemoryBudgetBytes)
	}
}

// The initial read of a fresh session, as JSON: sampled_at and heaviest are
// present and null, kills is an empty list, never null (Contract §3).
func TestTheInitialReadOfAFreshSessionSerializesAsTheContractSays(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	sess, _ := startHeadless(t, svc, w.task.ID)
	read, live := svc.SessionMemory(sess.ID)
	if !live {
		t.Fatal("a running session reads as not live")
	}
	m := jsonMap(t, read)
	for _, k := range []string{"kind", "session_id", "task_id", "sampled_at", "rss_bytes", "budget_bytes", "processes", "heaviest", "kills", "budget_source", "prevention"} {
		if _, ok := m[k]; !ok {
			t.Errorf("the read lacks %q: %v", k, m)
		}
	}
	if kills, ok := m["kills"].([]any); !ok || len(kills) != 0 {
		t.Errorf("kills = %#v, want []", m["kills"])
	}
	if m["sampled_at"] != nil || m["heaviest"] != nil || m["rss_bytes"] != float64(0) || m["processes"] != float64(0) {
		t.Errorf("read = %v, want null sampled_at and heaviest, nothing counted", m)
	}
	if m["kind"] != "session.memory" || m["budget_bytes"] != float64(4*gb) {
		t.Errorf("read = %v", m)
	}
}

// Samples of different sessions are throttled apart, a move of exactly 5 % is
// not more than 5 %, and a drop of more than 5 % is published at once.
func TestTheFeedThrottleIsPerSessionAndTheBoundaryIsMoreThanFivePercent(t *testing.T) {
	svc, _ := newInteractiveService(t)
	a, _ := startInteractive(t, svc, InteractiveRequest{})
	b, _ := startInteractive(t, svc, InteractiveRequest{})
	feed := follow(t, svc, "p1")
	t0 := time.Date(2026, 11, 25, 22, 40, 0, 0, time.UTC)
	count := func() (n int) {
		for {
			select {
			case c := <-feed:
				if c.Memory != nil {
					n++
				}
			case <-time.After(50 * time.Millisecond):
				return
			}
		}
	}

	svc.Sampled(sampleOf(a, t0, 1000*mb))
	svc.Sampled(sampleOf(b, t0.Add(time.Second), 1000*mb))
	if n := count(); n != 2 {
		t.Fatalf("first samples of two sessions published = %d, want 2", n)
	}
	svc.Sampled(sampleOf(a, t0.Add(2*time.Second), 1050*mb)) // exactly +5 %
	if n := count(); n != 0 {
		t.Errorf("a move of exactly 5 %% was published (%d)", n)
	}
	svc.Sampled(sampleOf(a, t0.Add(3*time.Second), 940*mb)) // -6 % against the last published
	if n := count(); n != 1 {
		t.Errorf("a drop of 6 %% published %d, want 1", n)
	}
}

// The last, empty sample of a stopped session reaches the stream and the
// project feed at once, even right after a sample the throttle would hold.
func TestTheEmptyLastSampleReachesTheFeedAtOnceWhateverTheThrottle(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	feed := follow(t, svc, "p1")
	live, _, cancel := svc.Subscribe(sess.ID)
	defer cancel()
	t0 := time.Date(2026, 11, 25, 22, 40, 0, 0, time.UTC)

	svc.Sampled(sampleOf(sess, t0, 1000*mb))
	if c := next(t, feed); c.Memory == nil || c.Memory.RSSBytes != 1000*mb {
		t.Fatalf("first feed change = %+v", c)
	}
	recvEvent(t, live)
	empty := domain.MemorySample{Kind: domain.MemorySampleKind, SessionID: sess.ID, TaskID: sess.TicketID, SampledAt: t0.Add(time.Second), Budget: 4 * gb}
	svc.Sampled(empty)
	c := next(t, feed)
	if c.Memory == nil || c.Memory.RSSBytes != 0 || c.Memory.Processes != 0 || c.Memory.Heaviest != nil {
		t.Errorf("feed change = %+v, want the empty last sample", c.Memory)
	}
	if m := jsonMap(t, recvEvent(t, live)); m["rss_bytes"] != float64(0) || m["processes"] != float64(0) || m["heaviest"] != nil {
		t.Errorf("stream event = %v, want the empty last sample", m)
	}
}

type fakeTable struct {
	mu   sync.Mutex
	rows []ports.ProcessInfo
}

func (f *fakeTable) Read(context.Context) ([]ports.ProcessInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.ProcessInfo(nil), f.rows...), nil
}

func (f *fakeTable) set(rows ...ports.ProcessInfo) { f.mu.Lock(); f.rows = rows; f.mu.Unlock() }

type fakeSignaler struct {
	mu   sync.Mutex
	sent []syscall.Signal
	pids []int
}

func (f *fakeSignaler) Signal(pid int, sig syscall.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pids, f.sent = append(f.pids, pid), append(f.sent, sig)
	return nil
}

// The whole path with the real vigia over a fake process table: a headless
// session over its orçamento has its heaviest process ended, the kill reaches
// the stream, the feed and the initial read, and stopping the session sends the
// empty last sample.
func TestTheVigiaOverTheServiceEndsTheHeaviestAndPublishesWhatTheContractNames(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	table, sig := &fakeTable{}, &fakeSignaler{}
	svc.MemoryWatcher = memorywatch.New(memorywatch.Deps{Table: table, Signal: sig, Sink: svc})
	sess, _ := startHeadless(t, svc, w.task.ID)
	feed := follow(t, svc, "p1")
	live, _, cancel := svc.Subscribe(sess.ID)
	defer cancel()

	rows := []ports.ProcessInfo{
		{PID: 100, PPID: 1, SID: 7, RSSBytes: 300 * mb, Name: "claude", Session: sess.ID},
		{PID: 201, PPID: 100, SID: 7, RSSBytes: 4500 * mb, Name: "node", Arg: "jest"},
		{PID: 202, PPID: 100, SID: 7, RSSBytes: 100 * mb, Name: "node", Arg: "tsc"},
	}
	table.set(rows...)
	watcher := svc.MemoryWatcher.(*memorywatch.Watcher)
	now := time.Date(2026, 11, 25, 22, 40, 0, 0, time.UTC)
	watcher.Poll(now)

	if m := jsonMap(t, recvEvent(t, live)); m["kind"] != "session.memory" || m["heaviest"].(map[string]any)["command"] != "node jest" {
		t.Errorf("sample event = %v", m)
	}
	sig.mu.Lock()
	if len(sig.pids) != 1 || sig.pids[0] != 201 || sig.sent[0] != syscall.SIGTERM {
		t.Errorf("signals = %v %v, want SIGTERM to 201", sig.pids, sig.sent)
	}
	sig.mu.Unlock()

	table.set(rows[0], rows[2]) // 201 has exited
	watcher.Poll(now.Add(time.Second))
	var killed bool
	// The notice told at SIGTERM puts a turn on the stream before the kill.
	for i := 0; i < 12 && !killed; i++ {
		if m := jsonMap(t, recvEvent(t, live)); m["kind"] == "session.memory_killed" {
			killed = m["pid"] == float64(201) && m["signal"] == "SIGTERM" && m["budget_bytes"] == float64(4*gb)
		}
	}
	if !killed {
		t.Error("no session.memory_killed for 201 on the session stream")
	}
	if read, _ := svc.SessionMemory(sess.ID); len(read.Kills) != 1 || read.Kills[0].PID != 201 {
		t.Errorf("initial read kills = %+v", read.Kills)
	}
	sawKill := false
	for i := 0; i < 6 && !sawKill; i++ {
		select {
		case c := <-feed:
			sawKill = c.MemoryKilled != nil && c.MemoryKilled.PID == 201
		case <-time.After(time.Second):
			i = 6
		}
	}
	if !sawKill {
		t.Error("the kill never reached the project feed")
	}

	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	empty := false
	for i := 0; i < 20 && !empty; i++ {
		select {
		case c := <-feed:
			empty = c.Memory != nil && c.Memory.RSSBytes == 0 && c.Memory.Processes == 0
		case <-time.After(time.Second):
			i = 20
		}
	}
	if !empty {
		t.Error("stopping the session never sent the empty last sample to the project feed")
	}
}

// A server-run session is handed to the vigia by its PTY's process with the
// orçamento it started with; a resume hands it again with the orçamento then in
// force and gives the new run no kills of the old one (user story 5).
func TestAResumedServerSessionIsWatchedAgainWithItsNewOrcamentoAndNoOldKills(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	savers := withTokenSavers(t, svc)
	fw := &fakeWatcher{}
	svc.MemoryWatcher = fw
	sess := startOnServer(t, svc)

	fw.mu.Lock()
	if len(fw.watched) != 1 || fw.watched[0].BudgetBytes != 4*gb || fw.watched[0].RootPID <= 0 || fw.watched[0].SessionID != sess.ID {
		t.Fatalf("watched = %+v, want the session by its PTY pid at 4 GB", fw.watched)
	}
	fw.mu.Unlock()
	svc.Killed(killOf(sess.ID))

	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	setGlobalBudget(t, savers, strconv.FormatInt(8*gb, 10))
	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID, RunsOn: domain.RunnerServer}); err != nil {
		t.Fatal(err)
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if len(fw.watched) != 2 || fw.watched[1].BudgetBytes != 8*gb || fw.watched[1].RootPID <= 0 {
		t.Fatalf("watched = %+v, want a second watch at 8 GB", fw.watched)
	}
	if fw.stopped < 1 {
		t.Errorf("the first run's watch was not stopped")
	}
	read, live := svc.SessionMemory(sess.ID)
	if !live || len(read.Kills) != 0 || read.Budget != 8*gb || read.Prevention.TestWorkers != 4 || read.BudgetSource != "global" {
		t.Errorf("initial read after the resume = live %v %+v, want no kills, 8 GB, 4 workers", live, read)
	}
}
