//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/gateruns"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Guard — the retry limit and escalation (ADR 0012): consecutive red
// declarations stop the session and tell the person once.

// personStop ends a session the way the person's Stop records it. The fixture
// has no terminal host, so the hosted stop itself (orchestration.Stop) is not
// what runs here; the escalation's own work is.
type personStop struct{ e *shellE2E }

func (p personStop) Stop(ctx context.Context, id string) error {
	_, err := p.e.orch.EndInteractive(ctx, id, 0, true)
	return err
}

// escalating makes the declarer's escalation stop sessions through personStop.
func (e *shellE2E) escalating() {
	e.declarer.Escalation.(*gateruns.Escalator).Stopper = personStop{e}
}

func (e *shellE2E) declare(id string) error {
	e.t.Helper()
	_, err := e.callTool(id, "declare_done", nil)
	return err
}

func (e *shellE2E) status(id string) domain.SessionStatus {
	return e.sessions.Get(id).Status
}

func (e *shellE2E) escalationNotices() []string {
	e.t.Helper()
	page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{ProjectID: e.project})
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, n := range page.Notifications {
		if strings.Contains(n.Title, "escalated session") {
			out = append(out, n.Title+" | "+n.Body)
		}
	}
	return out
}

func redGates() map[domain.GateKind]string {
	g := greenGates()
	g[domain.GateTest] = "echo FAIL; false"
	return g
}

func TestTheDeclarationThatReachesTheRetryLimitStopsTheSessionAndTellsThePersonOnce(t *testing.T) {
	e := newShellE2E(t)
	e.escalating()
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: redGates()})
	for i := 1; i <= 2; i++ {
		err := e.declare(sess.ID)
		if err == nil || !strings.Contains(err.Error(), "GATES_RED") || !strings.Contains(err.Error(), "of 3") {
			t.Fatalf("declaration %d: %v", i, err)
		}
		if e.status(sess.ID).IsTerminal() {
			t.Fatalf("the session stopped at declaration %d", i)
		}
	}
	if len(e.escalationNotices()) != 0 {
		t.Fatal("the person was told before the limit")
	}

	if err := e.declare(sess.ID); err == nil || !strings.Contains(err.Error(), "escalated") {
		t.Fatalf("third declaration: %v", err)
	}
	e.waitFor(sess.ID, func(st domain.SessionStatus) bool { return st == domain.SessionStopped })
	if got := e.status(sess.ID); got != domain.SessionStopped {
		t.Errorf("status = %q, want stopped", got)
	}
	notices := e.escalationNotices()
	if len(notices) != 1 || !strings.Contains(notices[0], "test") {
		t.Errorf("notifications = %q", notices)
	}
	runs := e.listedGateRuns(sess.ID)
	if last := runs[len(runs)-1]; !last.Escalated || last.Result != domain.GateRunRed {
		t.Errorf("the last run records no escalation: %+v", last)
	}
}

func TestRedRedGreenRedDoesNotEscalateAndPracticeAndPersonRunsLeaveTheCountAlone(t *testing.T) {
	e := newShellE2E(t)
	e.escalating()
	gates := greenGates()
	gates[domain.GateTest] = "test -f fixed"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})
	fixed := filepath.Join(sess.WorkingDir, "fixed")

	_ = e.declare(sess.ID)
	if _, err := e.callTool(sess.ID, "run_gates", nil); err != nil {
		t.Fatal(err)
	}
	e.runGates(sess.ID) // the person's run
	_ = e.declare(sess.ID)
	if e.status(sess.ID).IsTerminal() || len(e.escalationNotices()) != 0 {
		t.Fatal("practice and person runs counted")
	}

	if err := os.WriteFile(fixed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.declare(sess.ID); err != nil {
		t.Fatalf("the green declaration: %v", err)
	}
	if err := os.Remove(fixed); err != nil {
		t.Fatal(err)
	}
	_ = e.declare(sess.ID)
	if e.status(sess.ID).IsTerminal() || len(e.escalationNotices()) != 0 {
		t.Errorf("red, red, green, red escalated: %q", e.status(sess.ID))
	}
}

func TestALimitOfOneEscalatesOnTheFirstRedDeclarationAndAnEscalatedSessionCanBeResumed(t *testing.T) {
	e := newShellE2E(t)
	e.escalating()
	sess := e.gatedSession(&fakeAgentSystem{retry: 1, gates: redGates()})
	_ = e.declare(sess.ID)
	e.waitFor(sess.ID, func(st domain.SessionStatus) bool { return st == domain.SessionStopped })
	if e.status(sess.ID) != domain.SessionStopped {
		t.Fatalf("status = %q", e.status(sess.ID))
	}
	e.resume(sess.ID)
	e.waitFor(sess.ID, func(st domain.SessionStatus) bool { return !st.IsTerminal() })
	if e.status(sess.ID).IsTerminal() {
		t.Errorf("the escalated session did not resume: %q", e.status(sess.ID))
	}
}

func (e *shellE2E) resume(id string) {
	e.t.Helper()
	e.orch.Transcripts = transcriptsThatResume{}
	if sess := e.sessions.Get(id); !sess.Status.IsTerminal() {
		if _, err := e.orch.EndInteractive(e.ctx, id, 0, false); err != nil {
			e.t.Fatal(err)
		}
	}
	if _, _, err := e.orch.ResumeInteractive(e.ctx, ports.ResumeRequest{SessionID: id, RunsOn: domain.RunnerTUI}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *shellE2E) waitFor(id string, ok func(domain.SessionStatus) bool) {
	deadline := time.Now().Add(5 * time.Second)
	for !ok(e.status(id)) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}
