//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Guard — a delegate's done review request to its architect is its
// declaration (ADR 0012): refused while the gates are red, so the architect
// receives nothing.

// delegateOf starts an architect and an implement delegate under it, with the
// delegate's gates the fake settings give.
func (e *archE2E) delegateOf(settings *fakeAgentSystem) (architect, delegate string) {
	e.t.Helper()
	e.orch.AgentSystem = settings
	arch, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.ticket, Mode: "architect"})
	if err != nil {
		e.t.Fatal(err)
	}
	started := e.mustTool(arch.ID, "start_task_session", map[string]any{"prompt": "Build it.", "mode": "implement", "status_check_minutes": float64(0)})
	dev, _ := started["session_id"].(string)
	if dev == "" {
		e.t.Fatalf("start_task_session: %+v", started)
	}
	return arch.ID, dev
}

func (e *archE2E) messages() int {
	e.t.Helper()
	_, out := e.http("GET", "/api/task_messages?ticket_id="+e.ticket, "")
	list, _ := out["messages"].([]any)
	return len(list)
}

// One test: the fixture boots ExecutionModule, which registers Genkit flows once per process.
func TestAGatedDelegatesDoneReviewRequestIsRefusedWhileRedAndTheArchitectReceivesNothing(t *testing.T) {
	e := newArchE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "echo 'FAIL: TestThing'; test -f fixed"
	arch, dev := e.delegateOf(&fakeAgentSystem{retry: 3, gates: gates})

	_, err := e.tool(dev, "message_architect", map[string]any{"kind": "review_request", "subject": "Done", "body": "Ready."})
	if err == nil || !strings.Contains(err.Error(), "GATES_RED") || !strings.Contains(err.Error(), "FAIL: TestThing") {
		t.Fatalf("a red review request: %v", err)
	}
	if n := e.messages(); n != 0 {
		t.Errorf("the architect received %d messages", n)
	}

	// A status report that is not done, and a question, go through while red.
	e.mustTool(dev, "message_architect", map[string]any{"kind": "status_report", "status": "working", "body": "Still going."})
	e.mustTool(dev, "message_architect", map[string]any{"kind": "question", "body": "Which way?"})
	if n := e.messages(); n != 2 {
		t.Errorf("messages after a report and a question: %d, want 2", n)
	}

	// A done status report is a declaration too.
	if _, err := e.tool(dev, "message_architect", map[string]any{"kind": "status_report", "status": "done", "body": "Done."}); err == nil {
		t.Error("a done report was sent while red")
	}

	sess := e.sessions.Get(dev)
	if err := os.WriteFile(filepath.Join(sess.WorkingDir, "fixed"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.mustTool(dev, "message_architect", map[string]any{"kind": "review_request", "subject": "Done", "body": "Ready."})
	if n := e.messages(); n != 3 {
		t.Errorf("messages once green: %d, want 3", n)
	}

	// A session that is not gated has its review request delivered without a gate run.
	before := len(e.listRuns(dev))
	started := e.mustTool(arch, "start_task_session", map[string]any{"prompt": "Review it.", "mode": "review", "status_check_minutes": float64(0)})
	rev, _ := started["session_id"].(string)
	e.mustTool(rev, "message_architect", map[string]any{"kind": "review_request", "subject": "Review", "body": "Done."})
	if n := e.messages(); n != 4 {
		t.Errorf("messages after the reviewer's request: %d, want 4", n)
	}
	if got := len(e.listRuns(rev)); got != 0 || len(e.listRuns(dev)) != before {
		t.Errorf("gate runs: reviewer %d, implementer %d (was %d)", got, len(e.listRuns(dev)), before)
	}

	// An escalated delegate stops, and its architect is told once, naming the red gates.
	started = e.mustTool(arch, "start_task_session", map[string]any{"prompt": "Build more.", "mode": "implement", "status_check_minutes": float64(0)})
	dev2, _ := started["session_id"].(string)
	for i := 0; i < 3; i++ {
		if _, err := e.tool(dev2, "declare_done", nil); err == nil {
			t.Fatal("a red declaration was accepted")
		}
	}
	if st := e.sessions.Get(dev2).Status; st != domain.SessionStopped {
		t.Errorf("the escalated delegate is %q, want stopped", st)
	}
	_, out := e.http("GET", "/api/task_messages?ticket_id="+e.ticket, "")
	var escalations int
	for _, m := range out["messages"].([]any) {
		msg := m.(map[string]any)
		subject, _ := msg["subject"].(string)
		body, _ := msg["body"].(string)
		if strings.Contains(subject, "Escalated") {
			escalations++
			if msg["kind"] != "status_report" || msg["status"] != "blocked" || !strings.Contains(body, "test") || msg["from_session_id"] != dev2 {
				t.Errorf("escalation message = %v", msg)
			}
		}
	}
	if escalations != 1 {
		t.Errorf("the architect got %d escalation messages, want 1", escalations)
	}
}

func (e *archE2E) listRuns(sessionID string) []domain.GateRun {
	runs, err := e.gateRuns.List(e.ctx, sessionID)
	if err != nil {
		e.t.Fatal(err)
	}
	return runs
}
