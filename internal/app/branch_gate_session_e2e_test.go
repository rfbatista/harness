//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// What every agent session carries of the branch gate: the asking hook in its
// claude settings and the instruction (contract "Harness server ↔ Agent
// sessions — branch gate" §2 and §6).

func TestAnInteractiveSessionCarriesTheAskingHookBesideTheOthersAndTheInstruction(t *testing.T) {
	e := newShellE2E(t)
	e.orch.SessionBranchGateURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/branch_gate" }
	e.orch.SessionShellURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/shell" }
	e.orch.SessionArchitectureURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/architecture" }
	e.orch.BranchGateTimeout = 90 * time.Minute
	sess, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Hooks"), Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	asking := e.srv.URL + "/api/sessions/" + sess.ID + "/branch_gate?hook=PreToolUse"
	var found, others int
	for _, group := range spec.Hooks["PreToolUse"] {
		for _, h := range group.Hooks {
			if strings.Contains(h.Command, asking) {
				found++
				if group.Matcher != "Bash" || h.Timeout != 90*60 || !strings.Contains(h.Command, "-m 5400") {
					t.Errorf("the asking hook = matcher %q, timeout %d, %q", group.Matcher, h.Timeout, h.Command)
				}
				continue
			}
			others++
		}
	}
	if found != 1 || others < 2 {
		t.Errorf("PreToolUse hooks: %d asking, %d others, want 1 and the edit proxy's and the architecture rules'", found, others)
	}
	var instruction string
	for _, f := range spec.InstructionFiles {
		if f.Path == orchestration.BranchGateInstructionPath {
			instruction = string(f.Content)
		}
	}
	for _, want := range []string{"Stay on your session branch", "waits", "Force pushes and remote branch deletions are refused"} {
		if !strings.Contains(instruction, want) {
			t.Errorf("the instruction misses %q:\n%s", want, instruction)
		}
	}
}

func TestAHeadlessSessionWithNoTaskCarriesTheAskingHookToo(t *testing.T) {
	e := newShellE2E(t)
	e.orch.SessionBranchGateURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/branch_gate" }
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{orchestration.SettingSandbox: "off"}); err != nil {
		t.Fatal(err)
	}
	// This claude only writes down what it was started with.
	out := filepath.Join(t.TempDir(), "args")
	if err := os.WriteFile(e.cfg.ClaudeBin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >"+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sess, err := e.orch.Start(e.ctx, orchestration.StartRequest{ProjectID: e.project, RepositoryID: e.repo, Task: "no task"})
	if err != nil {
		t.Fatal(err)
	}
	want := "/api/sessions/" + sess.ID + "/branch_gate?hook=PreToolUse"
	eventually(t, "the headless claude's settings", func() error {
		b, _ := os.ReadFile(out)
		if !strings.Contains(string(b), want) {
			return fmt.Errorf("args %q lack %s", b, want)
		}
		return nil
	})
}
