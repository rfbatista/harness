//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Learn — the done declaration asks once for a task memory update (spec
// story 8). The check is another declaration check beside the gate run.

// memoryOffSettings is the fake guard configuration with the task memory
// check switched off.
type memoryOffSettings struct{ *fakeAgentSystem }

func (m memoryOffSettings) Effective(ctx context.Context, project, repo string) (domain.EffectiveAgentSystem, error) {
	eff, err := m.fakeAgentSystem.Effective(ctx, project, repo)
	eff.Learn.TaskMemory.RequireUpdate.Value = false
	return eff, err
}

func progressOf(e *shellE2E, sess *domain.Session) string {
	return filepath.Join(e.dataDir, "task-memory", sess.TicketID, e.repo, "progress.md")
}

func TestDeclareDoneAsksOnceForATaskMemoryUpdateThenRecordsTheOmission(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})

	_, err := e.callTool(sess.ID, "declare_done", nil)
	if err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") || !strings.Contains(err.Error(), "progress.md") {
		t.Fatalf("first declaration with progress.md untouched: %v", err)
	}
	out, err := e.callTool(sess.ID, "declare_done", nil)
	if err != nil || out["accepted"] != true {
		t.Fatalf("second declaration = %v, %v", out, err)
	}
	var recorded bool
	events, _ := e.sessions.ListEvents(sess.ID, 0)
	for _, ev := range events {
		recorded = recorded || ev.Type == "task_memory_omitted"
	}
	if !recorded {
		t.Errorf("the omission is not on the session's log: %v", events)
	}
}

func TestDeclareDoneAcceptsAnUpdatedProgressWithoutAsking(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	wrote := "# Progress\n\n## Done\n\n- the work\n\n## Failed and why\n\n## Next step\n\n## Open questions\n"
	if err := os.WriteFile(progressOf(e, sess), []byte(wrote), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := e.callTool(sess.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Errorf("declare_done = %v, %v", out, err)
	}
	events, _ := e.sessions.ListEvents(sess.ID, 0)
	for _, ev := range events {
		if ev.Type == "task_memory_omitted" {
			t.Error("an omission was recorded for an updated memory")
		}
	}
}

func TestDeclareDoneNeverChecksTheTaskMemoryWhenTheSettingIsOffOrTheSessionIsNotGated(t *testing.T) {
	e := newShellE2E(t)
	fake := &fakeAgentSystem{retry: 3, gates: greenGates()}
	off := e.gatedSession(fake)
	e.orch.AgentSystem = memoryOffSettings{fake}
	if out, err := e.callTool(off.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Errorf("setting off: %v, %v", out, err)
	}
}

func TestDeclareDoneNeverChecksTheTaskMemoryOfASessionThatIsNotGated(t *testing.T) {
	e := newShellE2E(t)
	e.orch.AgentSystem = &fakeAgentSystem{retry: 3, gates: greenGates()}
	plain, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Mode-less")})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if out, err := e.callTool(plain.ID, "declare_done", nil); err != nil || out["accepted"] != true {
			t.Fatalf("declaration %d = %v, %v", i+1, out, err)
		}
	}
}

func TestATaskMemoryRefusalRidesWithARedGateRunAndNeverCountsTowardTheRetryLimit(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "echo 'FAIL: TestThing'; false"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})
	_, err := e.callTool(sess.ID, "declare_done", nil)
	if err == nil || !strings.Contains(err.Error(), "GATES_RED") || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Fatalf("both refusals are expected together: %v", err)
	}
	var counted int
	for _, r := range e.listedGateRuns(sess.ID) {
		if r.Counted {
			counted++
		}
	}
	if counted != 1 {
		t.Errorf("counted runs = %d, want 1 (the memory refusal adds none)", counted)
	}
}

func (e *shellE2E) resumed(sess *domain.Session) {
	e.t.Helper()
	e.orch.Transcripts = transcriptsThatResume{}
	if _, err := e.orch.EndInteractive(e.ctx, sess.ID, 0, false); err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.orch.ResumeInteractive(e.ctx, ports.ResumeRequest{SessionID: sess.ID, RunsOn: domain.RunnerTUI}); err != nil {
		e.t.Fatal(err)
	}
}

// A resume keeps what the session was asked: an update made before it is not
// asked for again, and neither is a request already made.
func TestDeclareDoneAfterAResumeStillKnowsTheUpdateTheSessionMade(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	wrote := "# Progress\n\n## Done\n\n- before the resume\n\n## Failed and why\n\n## Next step\n\n## Open questions\n"
	if err := os.WriteFile(progressOf(e, sess), []byte(wrote), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.orch.TurnEnded(e.ctx, sess.ID, false); err != nil {
		t.Fatal(err)
	}
	e.resumed(sess)
	if out, err := e.callTool(sess.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Errorf("declare after a resume = %v, %v", out, err)
	}
}

func TestDeclareDoneAfterAResumeDoesNotAskAgainWhatWasAlreadyAsked(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	if _, err := e.callTool(sess.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Fatalf("first declaration: %v", err)
	}
	e.resumed(sess)
	if out, err := e.callTool(sess.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Errorf("declare after a resume = %v, %v", out, err)
	}
}

// The snapshot and the asked state survive a restart too.
func TestDeclareDoneAfterARestartStillAsksForAnUnchangedMemory(t *testing.T) {
	e := newShellE2EWith(t, "data.db")
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	e.restart()
	e.orch.AgentSystem = &fakeAgentSystem{retry: 3, gates: greenGates()}
	if _, err := e.callTool(sess.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Errorf("a restarted session with an unchanged memory: %v", err)
	}
}
