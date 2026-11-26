//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Guard — a delegate's done message to its architect is its declaration
// (ADR 0012): once its branch has a diff, a review request or a done or
// ready_for_review report is refused while the gates are red, so the architect
// receives nothing. The task's one ticket and its sessions are on the shell
// fixture, which has no terminal host: the sessions run in the client.

// architectWith starts the task's architect, and returns its ticket.
func (e *shellE2E) architectWith(settings *fakeAgentSystem) (ticket, architect string) {
	e.t.Helper()
	e.orch.AgentSystem = settings
	e.escalating()
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{orchestration.SettingSandbox: "off"}); err != nil {
		e.t.Fatal(err)
	}
	ticket = e.task("Delegated")
	arch, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: ticket, Mode: "architect"})
	if err != nil {
		e.t.Fatal(err)
	}
	return ticket, arch.ID
}

func (e *shellE2E) delegate(ticket, architect, mode string) *domain.Session {
	e.t.Helper()
	d, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: ticket, Mode: mode, ParentSessionID: architect})
	if err != nil {
		e.t.Fatal(err)
	}
	if d.Role != domain.RoleDelegate {
		e.t.Fatalf("role = %q, want delegate", d.Role)
	}
	return d
}

func (e *shellE2E) architectMessages(ticket string) []*domain.TaskMessage {
	e.t.Helper()
	msgs, err := e.channel.ListTaskMessages(e.ctx, ticket, ports.TaskMessageFilter{})
	if err != nil {
		e.t.Fatal(err)
	}
	return msgs
}

func TestAGatedDelegatesDoneMessagesAreRefusedWhileRedOnceItHasADiffAndTheArchitectReceivesNothing(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "echo 'FAIL: TestThing'; test -f fixed"
	ticket, arch := e.architectWith(&fakeAgentSystem{retry: 4, gates: gates})
	dev := e.delegate(ticket, arch, "implement")
	send := func(args map[string]any) (map[string]any, error) { return e.callTool(dev.ID, "message_architect", args) }
	messages := func() int { return len(e.architectMessages(ticket)) }

	// The ticket review before any code has no diff: it is no declaration, and
	// goes through while the gates are red, with no gate run.
	if _, err := send(map[string]any{"kind": "review_request", "subject": "Tickets", "body": "Please review my tickets."}); err != nil {
		t.Fatalf("the ticket review: %v", err)
	}
	if n, runs := messages(), len(e.listedGateRuns(dev.ID)); n != 1 || runs != 0 {
		t.Fatalf("a review request with no diff: %d messages, %d gate runs", n, runs)
	}

	// Once the branch has a diff, a done message is the declaration.
	if err := os.WriteFile(filepath.Join(dev.WorkingDir, "work.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := send(map[string]any{"kind": "review_request", "subject": "Done", "body": "Ready."})
	if err == nil || !strings.Contains(err.Error(), "GATES_RED") || !strings.Contains(err.Error(), "FAIL: TestThing") {
		t.Fatalf("a red review request: %v", err)
	}
	if n := messages(); n != 1 {
		t.Errorf("the architect received %d messages, want only the ticket review", n)
	}
	for _, status := range []string{"done", "ready_for_review"} {
		if _, err := send(map[string]any{"kind": "status_report", "status": status, "body": "Done."}); err == nil {
			t.Errorf("a %s report was sent while red", status)
		}
	}

	// A status report that is not done, and a question, go through while red.
	for _, m := range []map[string]any{
		{"kind": "status_report", "status": "working", "body": "Still going."},
		{"kind": "status_report", "status": "blocked", "body": "Stuck."},
		{"kind": "question", "body": "Which way?"},
	} {
		if _, err := send(m); err != nil {
			t.Errorf("%v: %v", m["kind"], err)
		}
	}
	if n := messages(); n != 4 {
		t.Errorf("messages after reports and a question: %d, want 4", n)
	}

	if err := os.WriteFile(filepath.Join(dev.WorkingDir, "fixed"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := send(map[string]any{"kind": "review_request", "subject": "Done", "body": "Ready."}); err != nil {
		t.Fatal(err)
	}
	if n := messages(); n != 5 {
		t.Errorf("messages once green: %d, want 5", n)
	}
}

func TestANonGatedDelegatesReviewRequestIsDeliveredWithoutAGateRunAndAnEscalatedOneTellsItsArchitectOnce(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "false"
	ticket, arch := e.architectWith(&fakeAgentSystem{retry: 3, gates: gates})

	rev := e.delegate(ticket, arch, "review")
	if err := os.WriteFile(filepath.Join(rev.WorkingDir, "work.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.callTool(rev.ID, "message_architect", map[string]any{"kind": "review_request", "subject": "Review", "body": "Done."}); err != nil {
		t.Fatal(err)
	}
	if runs := len(e.listedGateRuns(rev.ID)); runs != 0 || len(e.architectMessages(ticket)) != 1 {
		t.Errorf("gate runs %d, messages %d", runs, len(e.architectMessages(ticket)))
	}

	// An escalated delegate stops, and its architect is told once, naming the red gates.
	dev := e.delegate(ticket, arch, "implement")
	for i := 0; i < 3; i++ {
		if err := e.declare(dev.ID); err == nil {
			t.Fatal("a red declaration was accepted")
		}
	}
	e.waitFor(dev.ID, func(st domain.SessionStatus) bool { return st == domain.SessionStopped })
	if st := e.status(dev.ID); st != domain.SessionStopped {
		t.Errorf("the escalated delegate is %q, want stopped", st)
	}
	var escalations int
	for _, m := range e.architectMessages(ticket) {
		if !strings.Contains(m.Subject, "Escalated") {
			continue
		}
		escalations++
		if m.Kind != domain.MessageStatusReport || m.Status != domain.ReportBlocked || !strings.Contains(m.Body, "test") || m.FromSessionID != dev.ID {
			t.Errorf("escalation message = %+v", m)
		}
	}
	if escalations != 1 {
		t.Errorf("the architect got %d escalation messages, want 1", escalations)
	}
}
