//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/domain"
)

// Guard — the done declaration hunted adversarially (seam B): every message
// form, simultaneous declarations, a resume between red declarations, a gate
// that hangs, a gate that is noisy.

func TestEveryFormOfDoneMessageIsRefusedWhileRedOnceTheBranchHasADiffAndMalformedOnesAreNeverDelivered(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "echo FAIL-MARK; false"
	ticket, arch := e.architectWith(&fakeAgentSystem{retry: 50, gates: gates})
	dev := e.delegate(ticket, arch, "implement")
	send := func(args map[string]any) error {
		_, err := e.callTool(dev.ID, "message_architect", args)
		return err
	}
	delivered := func() int { return len(e.architectMessages(ticket)) }

	// With no diff, a done message is no declaration: it is delivered, no gate runs.
	for _, m := range []map[string]any{
		{"kind": "review_request", "body": "tickets"},
		{"kind": "status_report", "status": "done", "body": "d"},
		{"kind": "status_report", "status": "ready_for_review", "body": "d"},
	} {
		if err := send(m); err != nil {
			t.Errorf("without a diff %v: %v", m, err)
		}
	}
	if n, runs := delivered(), len(e.listedGateRuns(dev.ID)); n != 3 || runs != 0 {
		t.Fatalf("with no diff: %d delivered, %d gate runs", n, runs)
	}

	// With a diff: committed work, uncommitted work and an untracked file all count.
	if err := os.WriteFile(filepath.Join(dev.WorkingDir, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := delivered()
	forms := map[string]map[string]any{
		"review_request":                 {"kind": "review_request", "body": "d"},
		"review_request with a subject":  {"kind": "review_request", "subject": "Done", "body": "d", "document_ids": []any{"x"}},
		"status done":                    {"kind": "status_report", "status": "done", "body": "d"},
		"status ready_for_review":        {"kind": "status_report", "status": "ready_for_review", "body": "d"},
		"review_request with a reply id": {"kind": "review_request", "body": "d", "in_reply_to": "m1"},
	}
	for name, m := range forms {
		err := send(m)
		if err == nil || !strings.Contains(err.Error(), "GATES_RED") || !strings.Contains(err.Error(), "FAIL-MARK") {
			t.Errorf("%s: %v, want GATES_RED with the output", name, err)
		}
	}
	if got := delivered(); got != before {
		t.Errorf("%d message(s) reached the architect through red declarations", got-before)
	}

	// Malformed forms that a lenient reader might take for "done" are refused as
	// invalid input, and never delivered either.
	for name, m := range map[string]map[string]any{
		"kind in capitals":          {"kind": "REVIEW_REQUEST", "body": "d"},
		"kind with a space":         {"kind": " review_request", "body": "d"},
		"status in capitals":        {"kind": "status_report", "status": "DONE", "body": "d"},
		"status with a space":       {"kind": "status_report", "status": "done ", "body": "d"},
		"status on a review":        {"kind": "review_request", "status": "done", "body": "d"},
		"status on a question":      {"kind": "question", "status": "done", "body": "d"},
		"a reply kind":              {"kind": "reply", "body": "d"},
		"an unknown kind":           {"kind": "done", "body": "d"},
		"no kind":                   {"body": "d"},
	} {
		if err := send(m); err == nil {
			t.Errorf("%s was delivered", name)
		}
	}
	if got := delivered(); got != before {
		t.Errorf("a malformed message reached the architect: %d new", got-before)
	}

	// Not done: still allowed.
	for _, m := range []map[string]any{{"kind": "status_report", "status": "working", "body": "w"}, {"kind": "question", "body": "q"}} {
		if err := send(m); err != nil {
			t.Errorf("%v: %v", m, err)
		}
	}
}

func TestADeclarationThroughTheToolAndOneThroughTheMessageCountTowardTheSameLimit(t *testing.T) {
	e := newShellE2E(t)
	ticket, arch := e.architectWith(&fakeAgentSystem{retry: 3, gates: redGates()})
	dev := e.delegate(ticket, arch, "implement")
	if err := os.WriteFile(filepath.Join(dev.WorkingDir, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = e.declare(dev.ID)
	_, _ = e.callTool(dev.ID, "message_architect", map[string]any{"kind": "review_request", "body": "d"})
	if e.status(dev.ID).IsTerminal() {
		t.Fatalf("escalated after two reds: %q", e.status(dev.ID))
	}
	_, _ = e.callTool(dev.ID, "message_architect", map[string]any{"kind": "status_report", "status": "done", "body": "d"})
	e.waitFor(dev.ID, func(st domain.SessionStatus) bool { return st.IsTerminal() })
	if !e.status(dev.ID).IsTerminal() || len(e.escalationNotices()) != 1 {
		t.Errorf("after three red declarations by three routes: status %q, notices %v", e.status(dev.ID), e.escalationNotices())
	}
}

func TestAResumeBetweenRedDeclarationsDoesNotResetTheCount(t *testing.T) {
	e := newShellE2E(t)
	e.escalating()
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: redGates()})
	_ = e.declare(sess.ID)
	_ = e.declare(sess.ID)
	e.resume(sess.ID)
	e.waitFor(sess.ID, func(st domain.SessionStatus) bool { return !st.IsTerminal() })
	_ = e.declare(sess.ID)
	e.waitFor(sess.ID, func(st domain.SessionStatus) bool { return st.IsTerminal() })
	if !e.status(sess.ID).IsTerminal() || len(e.escalationNotices()) != 1 {
		t.Errorf("red, red, resume, red: status %q, notices %v", e.status(sess.ID), e.escalationNotices())
	}
}

func TestTwoDeclarationsAtOnceThroughTheServerTellThePersonOnce(t *testing.T) {
	e := newShellE2E(t)
	e.escalating()
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: redGates()})
	_ = e.declare(sess.ID)
	_ = e.declare(sess.ID)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.callTool(sess.ID, "declare_done", nil)
		}()
	}
	wg.Wait()
	e.waitFor(sess.ID, func(st domain.SessionStatus) bool { return st.IsTerminal() })
	if n := len(e.escalationNotices()); n != 1 {
		t.Errorf("two simultaneous red declarations at the limit sent %d notifications: %q", n, e.escalationNotices())
	}
}

func TestAHangingDeclarationGateIsKilledAtItsTimeoutAndCountsAsARedDeclaration(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateBuild] = "echo started; sleep 60"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates, timeouts: map[domain.GateKind]int{domain.GateBuild: 1}})
	err := e.declare(sess.ID)
	if err == nil || !strings.Contains(err.Error(), "GATES_RED") || !strings.Contains(err.Error(), "timed out after 1s") {
		t.Fatalf("a hanging gate's declaration: %v", err)
	}
	runs := e.listedGateRuns(sess.ID)
	if len(runs) != 1 || !runs[0].Counted || runs[0].Gates == nil {
		t.Errorf("runs = %+v", runs)
	}
}

func TestAGateWithHugeOutputIsStoredBoundedAndTheRefusalHandedToTheAgentIsBounded(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "i=0; while [ $i -lt 30 ]; do head -c 1048576 /dev/zero | tr '\\0' 'y'; i=$((i+1)); done; echo TAIL-MARK; exit 3"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates, timeouts: map[domain.GateKind]int{domain.GateTest: 60}})
	err := e.declare(sess.ID)
	if err == nil || !strings.Contains(err.Error(), "TAIL-MARK") {
		t.Fatalf("the refusal does not end with the gate's tail: %v", err)
	}
	if len(err.Error()) > 3*domain.MaxReportedOutputBytes {
		t.Errorf("the refusal handed to the agent is %d bytes", len(err.Error()))
	}
	for _, r := range e.listedGateRuns(sess.ID) {
		for _, g := range r.Gates {
			if len(g.Output) > domain.MaxGateOutputBytes {
				t.Errorf("a stored gate output holds %d bytes", len(g.Output))
			}
		}
	}
}
