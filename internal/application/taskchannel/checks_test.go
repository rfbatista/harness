package taskchannel_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func (f *fixture) check(t *testing.T, delegate string) *domain.StatusCheck {
	t.Helper()
	checks, _ := f.svc.ListStatusChecks(ctx, f.ticket)
	for _, c := range checks {
		if c.DelegateSessionID == delegate {
			return c
		}
	}
	return nil
}

func (f *fixture) fired(delivered bool) int {
	return f.feed.count(func(c ports.ProjectChange) bool {
		return c.StatusCheck != nil && c.StatusCheck.FiredAt != nil && *c.StatusCheck.Delivered == delivered
	})
}

func TestLoops_CreatedOnlyForTheArchitectsDirectDelegates(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	f.start(t, "fast", "arch", "", intp(2))
	f.start(t, "none", "arch", "", intp(0))
	f.start(t, "sub", "dev", "", nil)
	f.start(t, "peer", "", "", nil)
	f.start(t, "peerchild", "peer", "", nil)

	dev := f.check(t, "dev")
	if dev == nil || dev.EveryMinutes != 10 || dev.State != domain.StatusCheckActive || !dev.NextAt.Equal(f.clock.Add(10*time.Minute)) {
		t.Fatalf("default loop: %+v", dev)
	}
	if c := f.check(t, "fast"); c == nil || c.EveryMinutes != 2 {
		t.Fatalf("asked for 2: %+v", c)
	}
	for _, id := range []string{"none", "sub", "peer", "peerchild"} {
		if c := f.check(t, id); c != nil {
			t.Errorf("%s must have no loop: %+v", id, c)
		}
	}
	err := f.bus.Publish(ctx, domain.SessionStarted{SessionID: "x", TicketID: f.ticket, ParentSessionID: "arch", StatusCheckMinutes: intp(1)})
	if err == nil {
		t.Fatal("an out-of-range interval is refused")
	}
}

func TestLoops_FireIntoAnIdleArchitect(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	_ = f.sessions.UpdateMetrics("dev", 0, 0, 0, "tool: Edit", 0)

	f.advance(9 * time.Minute)
	f.svc.FireDue()
	if len(f.del.got("arch")) != 0 {
		t.Fatal("not due yet")
	}
	_, _ = f.svc.SendToArchitect(ctx, "dev", ports.TaskMessageInput{Kind: domain.MessageStatusReport, Status: domain.ReportBlocked, Body: "stuck"})
	f.advance(5 * time.Minute) // 14m: the report at 9m pushed it to 19m
	f.svc.FireDue()
	if n := len(f.del.got("arch")); n != 1 {
		t.Fatalf("only the report reached the architect, got %d turns", n)
	}
	f.advance(5 * time.Minute) // 19m
	f.svc.FireDue()
	turns := f.del.got("arch")
	if len(turns) != 2 {
		t.Fatalf("one check fired: %d", len(turns))
	}
	want := `[status check · claude session dev · status running · last action "tool: Edit" · last report status_report/blocked, 10m ago · pending reviews 0]`
	if !strings.HasPrefix(turns[1], want+"\n") {
		t.Fatalf("turn:\n%s\nwant prefix:\n%s", turns[1], want)
	}
	c := f.check(t, "dev")
	if c.FiredCount != 1 || c.LastFiredAt == nil || !c.NextAt.Equal(f.clock.Add(10*time.Minute)) || f.fired(true) != 1 {
		t.Fatalf("after firing: %+v", c)
	}
}

func TestLoops_BusyArchitectGetsOneTurnForMissedFirings(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", intp(2))
	f.del.busy["arch"] = true
	for range 3 {
		f.advance(2 * time.Minute)
		f.svc.FireDue()
	}
	if len(f.del.got("arch")) != 0 {
		t.Fatal("a busy architect is not interrupted")
	}
	f.del.endTurn("arch")
	if n := len(f.del.got("arch")); n != 1 {
		t.Fatalf("missed firings collapse into one turn, got %d", n)
	}
	if c := f.check(t, "dev"); c.FiredCount != 1 {
		t.Fatalf("fired once: %+v", c)
	}

	// After a restart, a loop many intervals late fires once.
	f.advance(time.Hour)
	f.svc.FireDue()
	if n := len(f.del.got("arch")); n != 2 {
		t.Fatalf("late loop fires once: %d", n)
	}
	if c := f.check(t, "dev"); !c.NextAt.Equal(f.clock.Add(2 * time.Minute)) {
		t.Fatalf("next firing from now: %+v", c.NextAt)
	}
}

func TestLoops_StoppedArchitectSkips(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	f.end(t, "arch", domain.SessionStopped)
	f.advance(10 * time.Minute)
	f.svc.FireDue()
	c := f.check(t, "dev")
	if c.State != domain.StatusCheckActive || c.FiredCount != 0 || !c.NextAt.Equal(f.clock.Add(10*time.Minute)) || f.fired(false) != 1 {
		t.Fatalf("a stopped architect keeps its loop, which skips: %+v", c)
	}
}

func TestLoops_EndWithTheDelegate(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	f.start(t, "gone", "arch", "", nil)
	f.end(t, "dev", domain.SessionFailed)
	if c := f.check(t, "dev"); c.State != domain.StatusCheckEnded || c.NextAt != nil {
		t.Fatalf("ended: %+v", c)
	}
	turns := f.del.got("arch")
	if len(turns) != 1 || !strings.Contains(turns[0], "· status ended: failed ·") {
		t.Fatalf("one last turn says how it ended: %q", turns)
	}
	_ = f.bus.Publish(ctx, domain.SessionDeleted{SessionID: "gone"})
	if c := f.check(t, "gone"); c.State != domain.StatusCheckEnded || len(f.del.got("arch")) != 1 {
		t.Fatalf("a deleted delegate's loop ends quietly: %+v", c)
	}
	f.advance(time.Hour)
	f.svc.FireDue()
	if len(f.del.got("arch")) != 1 {
		t.Fatal("ended loops never fire")
	}
}

func TestLoops_EndWhenAnotherArchitectTakesOver(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	f.start(t, "arch2", "", domain.SessionModeArchitect, nil)
	if c := f.check(t, "dev"); c.State != domain.StatusCheckEnded {
		t.Fatalf("the old architect's loop ends: %+v", c)
	}
	if len(f.del.got("arch")) != 0 {
		t.Fatal("with no last turn")
	}
}

func TestSetStatusCheck(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", intp(0))
	f.start(t, "sub", "dev", "", nil)

	_, err := f.svc.SetStatusCheckByPerson(ctx, "dev", 5)
	wantCode(t, err, "STATUS_CHECK_NOT_FOUND")
	_, err = f.svc.SetStatusCheck(ctx, "dev", "sub", 5)
	wantCode(t, err, "ARCHITECT_ONLY")
	_, err = f.svc.SetStatusCheck(ctx, "arch", "sub", 5)
	wantCode(t, err, "STATUS_CHECK_NOT_FOUND")
	_, err = f.svc.SetStatusCheck(ctx, "arch", "ghost", 5)
	wantCode(t, err, "SESSION_NOT_ON_TASK")
	_, err = f.svc.SetStatusCheck(ctx, "arch", "dev", 1)
	wantCode(t, err, "INVALID_INPUT")

	c, err := f.svc.SetStatusCheck(ctx, "arch", "dev", 5)
	if err != nil || c.State != domain.StatusCheckActive || c.EveryMinutes != 5 {
		t.Fatalf("the architect creates one for a direct delegate: %+v %v", c, err)
	}
	c, err = f.svc.SetStatusCheckByPerson(ctx, "dev", 0)
	if err != nil || c.State != domain.StatusCheckPaused || c.NextAt != nil {
		t.Fatalf("a person pauses it: %+v %v", c, err)
	}
	f.advance(time.Hour)
	f.svc.FireDue()
	if len(f.del.got("arch")) != 0 {
		t.Fatal("a paused loop does not fire")
	}
	c, _ = f.svc.SetStatusCheckByPerson(ctx, "dev", 30)
	if c.State != domain.StatusCheckActive || !c.NextAt.Equal(f.clock.Add(30*time.Minute)) {
		t.Fatalf("resumed: %+v", c)
	}
	f.end(t, "dev", domain.SessionDone)
	_, err = f.svc.SetStatusCheckByPerson(ctx, "dev", 10)
	wantCode(t, err, "INVALID_INPUT")
}

// The scheduler is one goroutine for all loops and stops with its context.
func TestRun_StopsWithItsContext(t *testing.T) {
	f := newFixture(t)
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.svc.Run(runCtx); close(done) }()
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}
