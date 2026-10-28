package taskchannel_test

import (
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// spun is a session a person started by hand on the task: a peer.
func (f *fixture) spun(t *testing.T, id string, status domain.SessionStatus, task string) {
	t.Helper()
	if _, err := f.sessions.Create(&domain.Session{
		ID: id, ProjectID: f.project, TicketID: f.ticket, Task: task, Status: status, Interactive: true,
		Branch: "feat/" + id, WorkingDir: "/w/" + id, Model: "m-" + id,
	}); err != nil {
		t.Fatal(err)
	}
	// The last action is recorded as the session runs, not at creation.
	if err := f.sessions.UpdateMetrics(id, 0, ports.SessionTokens{}, "Read x", 0); err != nil {
		t.Fatal(err)
	}
}

func skippedReasons(res *ports.BringUnderResult) map[string]domain.BringUnderSkip {
	out := map[string]domain.BringUnderSkip{}
	for _, s := range res.Skipped {
		out[s.SessionID] = s.Reason
	}
	return out
}

func TestBringUnder_MovesPeersUnderTheRunningArchitect(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.spun(t, "a", domain.SessionRunning, "build the thing")
	f.spun(t, "b", domain.SessionDone, "spike\nsecond line")
	before, _ := f.plan.GetTicket(ctx, f.ticket)

	res, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Architect.ID != "arch" || len(res.Moved) != 2 || res.Moved[0].ID != "b" || res.Moved[1].ID != "a" || len(res.Skipped) != 0 {
		t.Fatalf("result: %+v", res)
	}
	for _, m := range res.Moved {
		if m.Role != domain.RoleDelegate || m.ParentSessionID != "arch" || !m.BroughtUnder {
			t.Errorf("%s not a brought-under delegate: %+v", m.ID, m)
		}
	}

	// Moved, not copied, and nothing else about it changed.
	got := f.sessions.Get("a")
	if got.Branch != "feat/a" || got.WorkingDir != "/w/a" || got.Model != "m-a" || got.Task != "build the thing" || got.Status != domain.SessionRunning {
		t.Fatalf("a was changed beyond its parent: %+v", got)
	}
	if role, _ := f.svc.SessionRole(ctx, "b"); role != domain.RoleDelegate {
		t.Fatalf("role of b %q", role)
	}
	after, _ := f.plan.GetTicket(ctx, f.ticket)
	if after.Status != before.Status {
		t.Fatalf("task status moved: %s -> %s", before.Status, after.Status)
	}

	// A loop on the running one only.
	if c := f.check(t, "a"); c == nil || c.EveryMinutes != 10 || c.ArchitectSessionID != "arch" || c.State != domain.StatusCheckActive || !c.NextAt.Equal(f.clock.Add(10*time.Minute)) {
		t.Fatalf("loop on a: %+v", c)
	}
	if c := f.check(t, "b"); c != nil {
		t.Fatalf("an ended session gets no loop: %+v", c)
	}
	if _, err := f.svc.SetStatusCheck(ctx, "arch", "a", 30); err != nil {
		t.Fatalf("set_status_check accepts a moved session: %v", err)
	}
}

func TestBringUnder_StatusIsStillTheArchitects(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.spun(t, "a", domain.SessionRunning, "x")
	if _, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.SetTaskStatus(ctx, "a", domain.TicketStatusReview, "")
	wantCode(t, err, "TASK_STATUS_OWNED_BY_ARCHITECT")
}

func TestBringUnder_TurnsGoToTheArchitectAndTheRunningSessions(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.spun(t, "a", domain.SessionRunning, "build the thing")
	f.spun(t, "b", domain.SessionDone, "spike\nsecond line")
	f.clock = time.Now() // sessions are stamped with the real time
	f.advance(7 * time.Minute)

	if _, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}

	archTurns := f.del.got("arch")
	if len(archTurns) != 1 {
		t.Fatalf("one turn to the architect, got %d", len(archTurns))
	}
	wantArch := `[task update · sessions brought under you] The person brought 2 sessions of this task under you. Each is now your delegate: it reports to you with task messages, it cannot move the task status, and the ones still running have a status-check loop at the default interval.

- claude session a · status running · branch feat/a · brief "build the thing" · last action "Read x" · started 7m ago
- claude session b · status done · branch feat/b · brief "spike second line" · last action "Read x" · started 7m ago

Read their messages so far with list_task_messages and their branches with list_task_sessions. Supervise them as you do a delegate you started: answer what they report with reply_to_session, tune or pause each loop with set_status_check, ask the person for review with request_user_review when their work is ready, and move the task status as the work moves. Do not restart or duplicate their work.`
	if archTurns[0] != wantArch {
		t.Fatalf("architect turn:\n%s\nwant:\n%s", archTurns[0], wantArch)
	}

	aTurns := f.del.got("a")
	if len(aTurns) != 1 {
		t.Fatalf("one turn to a, got %d", len(aTurns))
	}
	if !strings.HasPrefix(aTurns[0], "[task update · architect] This task now has an architect: claude session arch. You are its delegate from now on.\n") {
		t.Fatalf("a's turn: %s", aTurns[0])
	}
	rest := strings.SplitN(domain.DelegateStatusSection, "\n", 2)[1]
	if !strings.HasSuffix(aTurns[0], rest+" Send it a status_report now, saying where your work stands.") {
		t.Fatalf("a's turn must carry the delegate status section verbatim and end with the request:\n%s", aTurns[0])
	}
	if len(f.del.got("b")) != 0 {
		t.Fatalf("an ended session gets no turn: %v", f.del.got("b"))
	}
}

func TestBringUnder_TurnsQueueBehindABusySession(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.spun(t, "a", domain.SessionRunning, "x")
	f.del.busy["a"], f.del.busy["arch"] = true, true
	if _, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if len(f.del.got("a")) != 0 || len(f.del.got("arch")) != 0 {
		t.Fatal("busy sessions are not interrupted")
	}
	f.del.endTurn("a")
	f.del.endTurn("arch")
	if len(f.del.got("a")) != 1 || len(f.del.got("arch")) != 1 {
		t.Fatal("the turns arrive when the current turn ends")
	}
}

func TestBringUnder_FeedAnnouncesSessionsThenLoopsThenTheArchitect(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.spun(t, "a", domain.SessionRunning, "x")
	f.spun(t, "b", domain.SessionRunning, "y")
	f.feed.mu.Lock()
	f.feed.changes = nil
	f.feed.mu.Unlock()

	if _, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range f.feed.changes {
		switch {
		case c.Session != nil:
			s := c.Session
			order = append(order, "session:"+s.ID)
			if s.ID != "arch" && (s.Role != domain.RoleDelegate || s.ParentSessionID != "arch" || !s.BroughtUnder || s.StatusCheck == nil) {
				t.Errorf("moved session change must carry role, parent, mark and loop: %+v", s)
			}
		case c.StatusCheck != nil:
			order = append(order, "check:"+c.StatusCheck.DelegateSessionID)
			if c.StatusCheck.State != domain.StatusCheckActive {
				t.Errorf("loop state %s", c.StatusCheck.State)
			}
		}
	}
	want := "session:a session:b check:a check:b session:arch"
	if strings.Join(order, " ") != want {
		t.Fatalf("feed order %v, want %s", order, want)
	}
}

func TestBringUnder_SkipsWhatIsNotEligibleAndRefusesAnEmptySet(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.start(t, "dev", "arch", "", nil)
	f.spun(t, "a", domain.SessionRunning, "x")
	res, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"arch", "dev", "ghost", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moved) != 1 || res.Moved[0].ID != "a" {
		t.Fatalf("moved %+v", res.Moved)
	}
	got := skippedReasons(res)
	if got["arch"] != domain.SkipIsArchitect || got["dev"] != domain.SkipAlreadyUnder || got["ghost"] != domain.SkipNotFound || len(got) != 3 {
		t.Fatalf("skipped %v", got)
	}
	if res.Skipped[0].SessionID != "arch" {
		t.Fatalf("skipped keep request order: %+v", res.Skipped)
	}

	_, err = f.svc.BringSessionsUnder(ctx, f.ticket, []string{"arch", "a", "ghost"})
	wantCode(t, err, "NOTHING_TO_MOVE")
	if d, ok := err.(*domain.DetailedError); !ok || len(d.Details.([]ports.BringUnderSkipped)) != 3 {
		t.Fatalf("NOTHING_TO_MOVE carries the skipped list: %#v", err)
	}
}

func TestBringUnder_WhatCountsAsOnTheTask(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	other, _ := f.plan.CreateTicket(ctx, f.project, "other", "", domain.TicketStatusTodo, "")
	if _, err := f.sessions.Create(&domain.Session{ID: "else", ProjectID: f.project, TicketID: other.ID, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	f.spun(t, "a", domain.SessionRunning, "x")
	res, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"else", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if skippedReasons(res)["else"] != domain.SkipNotOnTask || len(res.Moved) != 1 {
		t.Fatalf("result %+v", res)
	}
	if f.sessions.Get("else").ParentSessionID != "" {
		t.Fatal("another task's session was moved")
	}
}

func TestBringUnder_NeedsARunningArchitect(t *testing.T) {
	f := newFixture(t)
	f.spun(t, "a", domain.SessionRunning, "x")
	_, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"a"})
	wantCode(t, err, "NO_ARCHITECT")

	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.end(t, "arch", domain.SessionStopped)
	_, err = f.svc.BringSessionsUnder(ctx, f.ticket, []string{"a"})
	wantCode(t, err, "NO_ARCHITECT")

	_, err = f.svc.BringSessionsUnder(ctx, "nope", []string{"a"})
	wantCode(t, err, "TICKET_NOT_FOUND")
}

func TestBringUnder_MovedSessionKeepsItsMarkOnResume(t *testing.T) {
	f := newFixture(t)
	f.start(t, "arch", "", domain.SessionModeArchitect, nil)
	f.spun(t, "a", domain.SessionRunning, "x")
	if _, err := f.svc.BringSessionsUnder(ctx, f.ticket, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	f.end(t, "a", domain.SessionStopped)
	_ = f.sessions.UpdateStatus("a", domain.SessionRunning) // resumed
	if !f.sessions.Get("a").BroughtUnder || f.check(t, "a").State != domain.StatusCheckEnded {
		t.Fatal("the mark survives and a resume does not bring the loop back")
	}
}
