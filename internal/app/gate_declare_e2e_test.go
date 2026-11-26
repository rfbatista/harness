//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Guard — the done declaration and the practice run, called as the session
// over its task MCP endpoint (ADR 0012, 0014).

// callTool calls a session task tool as the session.
func (e *shellE2E) callTool(sessionID, name string, args map[string]any) (map[string]any, error) {
	e.t.Helper()
	for _, tl := range sessionTaskTools(e.cat, e.orch, e.plan, e.sessions, e.art, e.channel, nil, nil, tooling.WithDeclarations(e.declarer)) {
		if tl.Name != name {
			continue
		}
		res, err := tl.Handler(tooling.WithSessionID(context.Background(), sessionID), args)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(res)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return out, nil
	}
	e.t.Fatalf("%s is not served by the task server", name)
	return nil, nil
}

func TestDeclareDoneWhileAGateIsRedIsRefusedWithItsOutputAndAfterTheFixIsAccepted(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "echo 'FAIL: TestThing'; test -f fixed"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})

	_, err := e.callTool(sess.ID, "declare_done", nil)
	if err == nil {
		t.Fatal("a red gate accepted the declaration")
	}
	for _, want := range []string{"GATES_RED", "gate test failed", "exit code 1", "FAIL: TestThing", "test -f fixed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q lacks %q", err, want)
		}
	}

	if err := os.WriteFile(filepath.Join(sess.WorkingDir, "fixed"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := e.callTool(sess.ID, "declare_done", nil)
	if err != nil || out["accepted"] != true {
		t.Errorf("after the fix: %v, %v", out, err)
	}
	var kinds []string
	for _, r := range e.listedGateRuns(sess.ID) {
		kinds = append(kinds, string(r.Trigger)+"/"+string(r.Result))
	}
	if got := strings.Join(kinds, " "); got != "declaration/red declaration/green" {
		t.Errorf("recorded runs: %s", got)
	}
}

func TestRunGatesRecordsAPracticeRunThatNeverCountsAndCanBeLimitedToAKind(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "false"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})

	out, err := e.callTool(sess.ID, "run_gates", map[string]any{"kinds": []any{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	gs, _ := out["gates"].([]any)
	if out["result"] != "red" || out["counted"] != false || out["trigger"] != "practice" || len(gs) != 1 {
		t.Fatalf("run_gates = %v", out)
	}
	runs := e.listedGateRuns(sess.ID)
	if len(runs) != 1 || runs[0].Counted || len(runs[0].Kinds) != 1 || runs[0].Kinds[0] != domain.GateTest {
		t.Errorf("recorded %+v", runs)
	}
	if _, err := e.callTool(sess.ID, "run_gates", map[string]any{"kinds": []any{"nonsense"}}); err == nil || !strings.Contains(err.Error(), "INVALID_INPUT") {
		t.Errorf("an unknown kind: %v", err)
	}
}

func TestANonGatedSessionsDeclareDoneIsAcceptedAndRunsNothing(t *testing.T) {
	e := newShellE2E(t)
	e.orch.AgentSystem = &fakeAgentSystem{retry: 3, gates: map[domain.GateKind]string{domain.GateBuild: "false"}}
	plain, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Mode-less")})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.callTool(plain.ID, "declare_done", nil)
	if err != nil || out["accepted"] != true {
		t.Errorf("declare_done = %v, %v", out, err)
	}
	if runs := e.listedGateRuns(plain.ID); len(runs) != 0 {
		t.Errorf("ran %+v", runs)
	}
}
