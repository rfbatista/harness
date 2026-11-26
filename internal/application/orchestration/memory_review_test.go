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
