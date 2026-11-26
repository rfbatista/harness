//go:build !windows

package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Review findings F1, F6, F12, F15 on how the agent is told and how watches live.

// F1, the real order on the fake clock: SIGTERM goes out, the Bash call ends and
// answers, and only then does a sample see the process gone. The reason is in
// that call's answer and never reaches the courier as well.
func TestTheNoticeIsInThePostToolUseAnswerOfTheCallRunningWhenSIGTERMWentOut(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	table, sig := &fakeTable{}, &fakeSignaler{}
	watcher := memorywatch.New(memorywatch.Deps{Table: table, Signal: sig, Sink: svc})
	svc.MemoryWatcher = watcher
	sess, _ := startHeadless(t, svc, w.task.ID)
	// The fake CLI's first turn may still be running; its end clears the running
	// calls (no turn ends between a call's PreToolUse and PostToolUse), so the
	// call is started only once that turn is over.
	eventually(t, "the first turn to end", func() bool { return !svc.isBusy(sess.ID) })
	live, _, cancel := svc.Subscribe(sess.ID)
	defer cancel()

	rows := []ports.ProcessInfo{
		{PID: 100, PPID: 1, SID: 7, RSSBytes: 300 * mb, Name: "claude", Session: sess.ID},
		{PID: 201, PPID: 100, SID: 7, RSSBytes: 4500 * mb, Name: "node", Arg: "jest"},
	}
	table.set(rows...)
	now := time.Date(2026, 11, 25, 22, 40, 0, 0, time.UTC)

	svc.BashStarted(sess.ID, "call-1")
	watcher.Poll(now) // SIGTERM to 201
	got := svc.BashEnded(sess.ID, "call-1")
	for _, want := range []string{"node jest", "fewer workers", "not a failing test"} {
		if !strings.Contains(got, want) {
			t.Errorf("the answer of the call that was running lacks %q:\n%s", want, got)
		}
	}
	table.set(rows[0]) // 201 is gone by the next sample
	watcher.Poll(now.Add(time.Second))

	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-live:
			if ev.Type == "user_message" && strings.Contains(ev.Text, "memory budget") {
				t.Fatalf("the courier delivered the notice as well: %q", ev.Text)
			}
			continue
		case <-deadline:
		}
		break
	}
	if q := svc.courier.take(sess.ID); len(q) != 0 {
		t.Errorf("the courier holds %d parcels, want none", len(q))
	}
}

// F12: a call that began after SIGTERM went out is not the one the process
// belonged to; only the call running at that moment carries the notice.
func TestOnlyTheCallRunningWhenSIGTERMWentOutCarriesTheNotice(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})
	svc.BashStarted(sess.ID, "A")
	svc.Ending(killOf(sess.ID))
	svc.BashStarted(sess.ID, "B")
	if got := svc.BashEnded(sess.ID, "B"); got != "" {
		t.Errorf("a later call answers %q, want nothing", got)
	}
	if got := svc.BashEnded(sess.ID, "A"); !strings.Contains(got, "node jest") {
		t.Errorf("the running call answers %q, want the notice", got)
	}
}

// F6: a CLI that exits at once leaves no watch behind.
func TestACLIThatExitsAtOnceLeavesNoWatchRunning(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `exit 0`})
	withTokenSavers(t, svc)
	fw := &fakeWatcher{}
	svc.MemoryWatcher = fw
	sess := startOnServer(t, svc)
	waitStatus(t, svc, sess.ID, domain.SessionDone)
	deadline := time.Now().Add(5 * time.Second)
	for {
		fw.mu.Lock()
		w, s := len(fw.watched), fw.stopped
		fw.mu.Unlock()
		if w == s {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d watches installed, %d stopped: one outlives its session", w, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// F15: the kills of an ended session are freed.
func TestTheKillsOfAnEndedSessionAreFreed(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	svc.MemoryWatcher = &fakeWatcher{}
	sess, _ := startHeadless(t, svc, w.task.ID)
	svc.Killed(killOf(sess.ID))
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		svc.memory.mu.Lock()
		n := len(svc.memory.kills[sess.ID])
		svc.memory.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d kills kept for an ended session", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// F17: a product owner session runs an agent CLI, so it has an orçamento, its
// prevenção and a watch, like any other session.
func TestAProductOwnerSessionIsWatchedAndGetsItsPrevention(t *testing.T) {
	svc, _ := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	fw := &fakeWatcher{}
	svc.MemoryWatcher = fw
	id, err := svc.StartProductOwnerSession(context.Background(), ports.ProductOwnerSessionSpec{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), id) })
	fw.mu.Lock()
	if len(fw.watched) != 1 || fw.watched[0].SessionID != id || fw.watched[0].BudgetBytes != 4*gb || fw.watched[0].ClaudeID != id {
		t.Errorf("watched = %+v, want the product owner session at 4 GB", fw.watched)
	}
	fw.mu.Unlock()
	if sess := svc.sessions.Get(id); sess == nil || sess.MemoryBudgetBytes != 4*gb || sess.MemoryTestWorkers != 2 || sess.MemoryHeapMB != 1365 {
		t.Errorf("session = %+v, want its budget and prevention recorded", sess)
	}
}

// F6, the race itself: when the CLI has already exited by the time the session
// is handed to the vigia, no watch is installed (it would run for good).
func TestASessionThatHasAlreadyEndedIsNotHandedToTheVigia(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	withTokenSavers(t, svc)
	fw := &fakeWatcher{}
	svc.MemoryWatcher = fw
	sess := startOnServer(t, svc)
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, svc, sess.ID, domain.SessionStopped)
	fw.mu.Lock()
	before := len(fw.watched)
	fw.mu.Unlock()

	svc.watchTerminalMemory(sess.ID)

	fw.mu.Lock()
	defer fw.mu.Unlock()
	if len(fw.watched) != before {
		t.Errorf("a watch was installed for an ended session: %d → %d", before, len(fw.watched))
	}
}

// N5: between a session's cleanup and its terminal status, a watch handed over
// would outlive it. Once cleanup has run for a run, nothing is watched for it.
func TestNoWatchIsInstalledOnceTheRunsCleanupHasRun(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	fw := &fakeWatcher{}
	svc.MemoryWatcher = fw
	sess, _ := startHeadless(t, svc, w.task.ID)
	fw.mu.Lock()
	before := len(fw.watched)
	fw.mu.Unlock()

	svc.endRun(sess.ID) // the status is still running: the window of the race
	svc.watchMemory(sess.ID, "", 4*gb, sess.ID, 0)

	fw.mu.Lock()
	defer fw.mu.Unlock()
	if len(fw.watched) != before {
		t.Errorf("a watch was installed after the cleanup ran: %d → %d", before, len(fw.watched))
	}
}

// A resume is a new run: its watch is installed although an earlier run's cleanup ran.
func TestAResumeIsWatchedAlthoughTheEarlierRunsCleanupRan(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	withTokenSavers(t, svc)
	fw := &fakeWatcher{}
	svc.MemoryWatcher = fw
	sess := startOnServer(t, svc)
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, svc, sess.ID, domain.SessionStopped)
	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID, RunsOn: domain.RunnerServer}); err != nil {
		t.Fatal(err)
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if len(fw.watched) != 2 {
		t.Errorf("watches = %d, want one per run", len(fw.watched))
	}
}

// N6: a product owner thread is project-level: the orçamento is the global one,
// whatever a repository overrides.
func TestAProductOwnerSessionUsesTheGlobalOrcamentoOnly(t *testing.T) {
	svc, _ := newHeadlessShellService(t)
	savers := withTokenSavers(t, svc)
	setOverride(t, savers, domain.ActivationLevelRepository, "r1", 8*gb)
	id, err := svc.StartProductOwnerSession(context.Background(), ports.ProductOwnerSessionSpec{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), id) })
	sess := svc.sessions.Get(id)
	if sess.MemoryBudgetBytes != 4*gb || sess.MemoryBudgetSource != "global" {
		t.Errorf("budget = %d from %q, want 4 GB from global", sess.MemoryBudgetBytes, sess.MemoryBudgetSource)
	}
}

// L1: a process the vigia gave up on is not "killed": no kill is recorded or
// published for it.
func TestAProcessGivenUpOnLeavesNoKillRecord(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})
	k := killOf(sess.ID)
	svc.Ending(k)
	svc.KillFailed(k)
	if read, _ := svc.SessionMemory(sess.ID); len(read.Kills) != 0 {
		t.Errorf("kills = %+v, want none for a process that could not be ended", read.Kills)
	}
}

// midInstallWatcher ends the session's run while the watch is being installed:
// the race between the check and the storing of the cleanup.
type midInstallWatcher struct {
	fakeWatcher
	svc *Service
}

func (m *midInstallWatcher) Watch(w ports.MemoryWatch) func() {
	m.svc.endRun(w.SessionID)
	return m.fakeWatcher.Watch(w)
}

// L2: an end that comes between the run check and the storing of the cleanup is
// seen too: the watch is stopped at once.
func TestAWatchInstalledWhileTheRunEndsIsStoppedAtOnce(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	sess, _ := startHeadless(t, svc, w.task.ID)
	mw := &midInstallWatcher{svc: svc}
	svc.MemoryWatcher = mw
	svc.reopenRun(sess.ID) // an earlier endRun of the run does not apply to the one under test

	svc.watchMemory(sess.ID, "", 4*gb, sess.ID, 0)

	mw.mu.Lock()
	defer mw.mu.Unlock()
	if len(mw.watched) != 1 || mw.stopped != 1 {
		t.Errorf("watches %d, stopped %d: a watch installed as the run ended outlives it", len(mw.watched), mw.stopped)
	}
}

// L3: the run marks are dropped once the terminal status is written; they only
// guard the gap before it.
func TestTheRunMarkOfAnEndedSessionIsPruned(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	withTokenSavers(t, svc)
	svc.MemoryWatcher = &fakeWatcher{}
	sess := startOnServer(t, svc)
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, svc, sess.ID, domain.SessionStopped)
	eventually(t, "the run mark to be pruned", func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return !svc.cleaned[sess.ID]
	})
}
