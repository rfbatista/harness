//go:build !windows

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// pendingRequests is the branch gate's store as the boot reads it: the
// pending requests of each session.
type pendingRequests map[string]int

func (p pendingRequests) PendingBranchRequests(_ context.Context, sessionID string) int {
	return p[sessionID]
}

// eventsOf are the logged events of a session of the given type.
func eventsOf(t *testing.T, svc *Service, id, typ string) []SessionEvent {
	t.Helper()
	stored, err := svc.sessions.ListEvents(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []SessionEvent
	for _, e := range stored {
		if e.Type != typ {
			continue
		}
		var ev SessionEvent
		if err := json.Unmarshal(e.Payload, &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func orphan(t *testing.T, svc *Service, id string, status domain.SessionStatus) {
	t.Helper()
	if _, err := svc.sessions.Create(&domain.Session{
		ID: id, ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Status: status,
		Interactive: true, RunsOn: domain.RunnerServer,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.sessions.UpdateMetrics(id, 0, ports.SessionTokens{}, "Edit main.go", 0); err != nil {
		t.Fatal(err)
	}
}

// An interrupted session was mid-turn when the server stopped (ADR 0082): the
// boot reads the stored status, so a crash loses nothing.
func TestBoot_MarksTheSessionsThatWereMidTurn(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	svc.BranchRequests = pendingRequests{"orphan-" + string(domain.SessionWaitingApproval): 2}
	for _, status := range domain.LiveSessionStatuses {
		orphan(t, svc, "orphan-"+string(status), status)
	}
	tui, _ := startInteractive(t, svc, InteractiveRequest{})

	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	midTurn := map[domain.SessionStatus]bool{
		domain.SessionStarting: true, domain.SessionRunning: true,
		domain.SessionThinking: true, domain.SessionWaitingApproval: true,
	}
	for _, status := range domain.LiveSessionStatuses {
		id := "orphan-" + string(status)
		got := sessionOf(svc, id)
		if got.Status != domain.SessionStopped {
			t.Errorf("%s = %s, want stopped", id, got.Status)
		}
		if marked := got.InterruptedAt != nil; marked != midTurn[status] {
			t.Errorf("%s interrupted = %v, want %v", id, marked, midTurn[status])
		}
		evs := eventsOf(t, svc, id, "interrupted_by_restart")
		if midTurn[status] != (len(evs) == 1) {
			t.Fatalf("%s has %d interrupted_by_restart events", id, len(evs))
		}
		if len(evs) == 1 {
			e := evs[0]
			if e.PreviousStatus != status || e.LastAction != "Edit main.go" {
				t.Errorf("%s event = %+v, want previous %s and last action", id, e, status)
			}
			want := map[bool]int{true: 2}[status == domain.SessionWaitingApproval]
			if e.ExpiredBranchRequests == nil || *e.ExpiredBranchRequests != want {
				t.Errorf("%s expired branch requests = %v, want %d", id, e.ExpiredBranchRequests, want)
			}
		}
	}
	if got := sessionOf(svc, tui.ID); got.Status != domain.SessionRunning || got.InterruptedAt != nil {
		t.Errorf("tui-client session = %s marked %v, want running and unmarked", got.Status, got.InterruptedAt)
	}
}

// The shutdown step decides by the same rule as the boot.
func TestShutdown_MarksTheSessionsThatAreMidTurn(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	busy := startOnServer(t, svc)
	idle := startOnServer(t, svc)
	if err := svc.sessions.UpdateStatus(idle.ID, domain.SessionIdle); err != nil {
		t.Fatal(err)
	}

	svc.StopServerSessions(context.Background())
	waitStatus(t, svc, busy.ID, domain.SessionStopped)
	waitStatus(t, svc, idle.ID, domain.SessionStopped)
	if sessionOf(svc, busy.ID).InterruptedAt == nil {
		t.Error("a running session was not marked at shutdown")
	}
	if sessionOf(svc, idle.ID).InterruptedAt != nil {
		t.Error("an idle session was marked at shutdown")
	}
}

// A session that was stopped by the person, not by a restart, is not marked.
func TestAStopTheServerDidNotMakeLeavesNoMark(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	sess := startOnServer(t, svc)
	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, svc, sess.ID, domain.SessionStopped)
	if sessionOf(svc, sess.ID).InterruptedAt != nil {
		t.Error("a session the person stopped carries the mark")
	}
}

// A resume of a marked session sends the continue turn (ADR 0082).
func TestResume_OfAnInterruptedSessionSendsTheContinueTurn(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.BranchRequests = pendingRequests{}
	orphan(t, svc, "s1", domain.SessionWaitingApproval)
	svc.BranchRequests = pendingRequests{"s1": 1}
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A message that waited for the session while it was stopped.
	svc.courier.enqueue("s1", parcel{key: "m1", text: "waiting message"})

	got, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"restarted", "check the state of your last action", "Edit main.go", string(domain.SessionWaitingApproval), "1 branch request"} {
		if !strings.Contains(spec.Prompt, want) {
			t.Errorf("continue turn %q lacks %q", spec.Prompt, want)
		}
	}
	if got.InterruptedAt != nil {
		t.Error("the resume left the mark")
	}
	if n := len(eventsOf(t, svc, "s1", "resumed_with_continue_turn")); n != 1 {
		t.Errorf("%d resumed_with_continue_turn events, want 1", n)
	}
	// The waiting message comes after the continue turn, as its own turn.
	if turn, err := svc.TurnEnded(context.Background(), "s1", false); err != nil || turn != "waiting message" {
		t.Errorf("turn after the continue turn = %q, %v; want the waiting message", turn, err)
	}
}

// An unmarked session resumes idle: no turn, no tokens.
func TestResume_OfAnUnmarkedSessionSendsNoTurn(t *testing.T) {
	svc, _ := newInteractiveService(t)
	orphan(t, svc, "s1", domain.SessionIdle)
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Prompt != "" {
		t.Errorf("an unmarked resume sent %q", spec.Prompt)
	}
	if n := len(eventsOf(t, svc, "s1", "resumed_with_continue_turn")); n != 0 {
		t.Errorf("%d resumed_with_continue_turn events, want 0", n)
	}
}

// A restart during the resumed turn marks the session again.
func TestAResumedSessionStoppedMidTurnIsMarkedAgain(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	orphan(t, svc, "s1", domain.SessionRunning)
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1", RunsOn: domain.RunnerServer}); err != nil {
		t.Fatal(err)
	}
	// The server dies again: the resumed session is stored running, and its
	// process died with the server.
	fresh := termhost.New(shell.Direct{}, ptyunix.New(), scriptAgent{script: `cat`})
	t.Cleanup(func() { _ = fresh.Shutdown(context.Background()) })
	svc.Terminals = fresh
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := sessionOf(svc, "s1")
	if got.Status != domain.SessionStopped || got.InterruptedAt == nil {
		t.Errorf("session = %s marked %v, want stopped and marked", got.Status, got.InterruptedAt)
	}
	if n := len(eventsOf(t, svc, "s1", "interrupted_by_restart")); n != 2 {
		t.Errorf("%d interrupted_by_restart events, want 2", n)
	}
}

// The events a restart records come after the ones the session already holds,
// so the history reads in order and a client that follows by seq misses none.
func TestTheRestartsEventsContinueTheSessionsLog(t *testing.T) {
	svc, _ := newInteractiveService(t)
	orphan(t, svc, "s1", domain.SessionThinking)
	svc.publish("s1", SessionEvent{Type: "output", Text: "working"})
	svc.publish("s1", SessionEvent{Type: "output", Text: "still working"})
	before, err := svc.sessions.LastEventSeq("s1")
	if err != nil || before != 2 {
		t.Fatalf("log of the session ends at seq %d, %v; want 2", before, err)
	}
	svc.seq = map[string]int64{} // a new server: nothing is counted in memory
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.sessions.ListEvents("s1", before)
	if err != nil || len(stored) < 2 {
		t.Fatalf("events after the restart = %+v, %v", stored, err)
	}
	for i, ev := range stored {
		if ev.Seq != before+int64(i)+1 {
			t.Errorf("event %d (%s) has seq %d, want %d", i, ev.Type, ev.Seq, before+int64(i)+1)
		}
	}
}

// A resume that cannot start the agent leaves the session stopped and marked,
// so the next resume (or Resume all) tries it again.
func TestAResumeThatFailsToStartTheAgentKeepsTheSessionInterrupted(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`, err: errors.New("no such agent")})
	orphan(t, svc, "s1", domain.SessionThinking)
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1", RunsOn: domain.RunnerServer}); err == nil {
		t.Fatal("the resume succeeded with an agent that cannot start")
	}
	got := sessionOf(svc, "s1")
	if got.Status != domain.SessionStopped || got.InterruptedAt == nil {
		t.Errorf("session = %s marked %v, want stopped and still marked", got.Status, got.InterruptedAt)
	}
	// The next resume, with an agent that starts, still finds the mark.
	svc.Terminals = termhost.New(shell.Direct{}, ptyunix.New(), scriptAgent{script: `cat`})
	if _, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1", RunsOn: domain.RunnerServer}); err != nil || spec.Prompt == "" {
		t.Errorf("the retry = %v with prompt %q, want a resume that sends the continue turn", err, spec.Prompt)
	}
}

// The sessions an agent starts with start_task_session (architect, product,
// implement and a plain peer) are interactive and run on the server, so a
// restart marks them (ADR 0082).
func TestEverySessionAnAgentStartsIsOneARestartMarks(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `cat`})
	var ids []string
	for _, mode := range []string{"", "architect", "product", "implement"} {
		sess, _ := startInteractive(t, svc, InteractiveRequest{Mode: mode, ParentSessionID: "parent", RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 80, Rows: 24}})
		if !sess.Interactive || sess.RunsOn != domain.RunnerServer {
			t.Fatalf("mode %q started interactive=%v on %q", mode, sess.Interactive, sess.RunsOn)
		}
		ids = append(ids, sess.ID)
	}
	// A new server: no process holds them, and each is stored mid-turn.
	svc.Terminals = termhost.New(shell.Direct{}, ptyunix.New(), scriptAgent{script: `cat`})
	for _, id := range ids {
		if err := svc.sessions.UpdateStatus(id, domain.SessionThinking); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if got := sessionOf(svc, id); got.Status != domain.SessionStopped || got.InterruptedAt == nil {
			t.Errorf("session %s = %s marked %v, want stopped and marked", id, got.Status, got.InterruptedAt)
		}
	}
}
