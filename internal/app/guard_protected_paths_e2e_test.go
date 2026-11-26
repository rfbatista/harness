//go:build !windows

package app

import (
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// Guard — protected paths (ADR 0012): a gated session's write to a path its
// kept configuration protects is refused by the same hook and PATH shims as the
// architecture rules, and the refusal is recorded and announced.

const protectedMessage = "PROTECTED_PATH: check.sh matches the protected path check.sh"

// protectedSession starts a session of mode with the shims on its PATH, over the
// default protected paths.
func (e *shellE2E) protectedSession(bin string, mode domain.SessionMode, realURL bool) shimSession {
	e.t.Helper()
	e.orch.AgentSystem = &fakeAgentSystem{retry: 3, gates: greenGates()}
	if realURL { // the shims call the test server; the hook tests rewrite the configured base
		e.orch.SessionArchitectureURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/architecture" }
	}
	return e.startWithShimsMode(e.task("Protected"), bin, mode)
}

func (e *shellE2E) ruleEvents() []map[string]any {
	e.t.Helper()
	var out struct {
		Events []map[string]any `json:"events"`
	}
	e.post("list_architecture_rule_events", map[string]any{"repository_id": e.repo}, &out)
	return out.Events
}

func TestAGatedSessionsWriteToAProtectedPathIsRefusedByTheHookAndByTheShellAndRecorded(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	a := e.protectedSession(bin, domain.SessionModeImplement, false)
	feed, err := e.orch.FollowProject(e.ctx, e.project)
	if err != nil {
		t.Fatal(err)
	}
	check := filepath.Join(a.worktree, "check.sh")
	write(t, check, "echo ok\n")
	workflow := filepath.Join(a.worktree, ".github", "workflows", "ci.yml")

	// The file tools, through the hook, by absolute and relative path.
	for _, tool := range []string{"Edit", "Write", "MultiEdit"} {
		out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, editInput(tool, check))
		if !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, protectedMessage) {
			t.Errorf("%s of check.sh = %s", tool, out)
		}
	}
	out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", ".github/workflows/ci.yml"))
	if !strings.Contains(out, "PROTECTED_PATH: .github/workflows/ci.yml matches the protected path .github/workflows/**") {
		t.Errorf("a path under a ** glob = %s", out)
	}

	// A redirect in a Bash command, through the hook.
	for _, cmd := range []string{"echo x > check.sh", "echo x >> check.sh", "cat <<EOF > .github/workflows/ci.yml\nx\nEOF"} {
		out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, bashInput(a.worktree, cmd))
		if !strings.Contains(out, "PROTECTED_PATH") {
			t.Errorf("%q through the Bash hook = %s", cmd, out)
		}
	}

	// A write to an unprotected path goes through, as does a command that only reads.
	for _, in := range []map[string]any{
		editInput("Write", filepath.Join(a.worktree, "main.go")),
		bashInput(a.worktree, "echo x > notes.txt"),
		bashInput(a.worktree, "cat check.sh"),
	} {
		if out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, in); out != "" {
			t.Errorf("an allowed action = %s", out)
		}
	}

	// The PATH shims, in a second session that calls the test server directly.
	hooksID := a.id
	a = e.protectedSession(bin, domain.SessionModeImplement, true)
	check = filepath.Join(a.worktree, "check.sh")
	write(t, check, "echo ok\n")
	workflow = filepath.Join(a.worktree, ".github", "workflows", "ci.yml")
	// sed -i, tee, cp, mv, rm and touch refuse, and the file is as it was.
	write(t, filepath.Join(a.worktree, "other.sh"), "other\n")
	for name, line := range map[string]string{
		"sed -i": "sed " + inPlace() + " 's/ok/bad/' check.sh",
		"tee":    "printf 'bad\\n' | tee check.sh",
		"cp":     "cp other.sh check.sh",
		"mv":     "mv other.sh check.sh",
		"rm":     "rm check.sh",
		"touch":  "touch .github/workflows/new.yml",
	} {
		r := a.inWorktree(t, line)
		if r.code != 1 || !strings.Contains(r.stderr, "PROTECTED_PATH") {
			t.Errorf("%s: %+v, want exit 1 with PROTECTED_PATH", name, r)
		}
	}
	if got := read(t, check); got != "echo ok\n" {
		t.Errorf("check.sh = %q after the refused writes", got)
	}
	if got := read(t, workflow); got != "<missing>" {
		t.Errorf("a refused touch created %s", workflow)
	}
	if r := a.inWorktree(t, "printf 'x\\n' | tee notes.txt"); r.code != 0 {
		t.Errorf("an unprotected tee: %+v", r)
	}

	// Every refusal is recorded with the session, and announced.
	perSession := map[string]int{}
	for _, ev := range e.ruleEvents() {
		if ev["rule_id"] != "protected_path" {
			continue
		}
		perSession[ev["session_id"].(string)]++
		if ev["outcome"] != "blocked" || ev["at_point"] != "before_tool" || ev["detail"] == "" {
			t.Errorf("event = %v", ev)
		}
	}
	if perSession[hooksID] != 7 || perSession[a.id] != 6 {
		t.Errorf("recorded refusals per session = %v, want 7 for the hook session and 6 for the shim session", perSession)
	}
	var announced bool
drain:
	for {
		select {
		case ch := <-feed:
			if ch.ArchitectureRuleEvent != nil && ch.ArchitectureRuleEvent.Event.RuleID == "protected_path" {
				announced = true
			}
		default:
			break drain
		}
	}
	if !announced {
		t.Error("no protected-path refusal reached the project feed")
	}
}

func TestASessionThatIsNotGatedMayWriteAProtectedPath(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	a := e.protectedSession(bin, domain.SessionModeOperate, false)
	if out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", filepath.Join(a.worktree, "check.sh"))); out != "" {
		t.Errorf("a non-gated Edit of check.sh = %s", out)
	}
	e.orch.SessionArchitectureURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/architecture" }
	b := e.protectedSession(bin, domain.SessionModeOperate, true)
	write(t, filepath.Join(b.worktree, "check.sh"), "echo ok\n")
	if r := b.inWorktree(t, "printf 'new\\n' | tee check.sh"); r.code != 0 || read(t, filepath.Join(b.worktree, "check.sh")) != "new\n" {
		t.Errorf("a non-gated tee: %+v", r)
	}
	for _, ev := range e.ruleEvents() {
		if ev["rule_id"] == "protected_path" {
			t.Errorf("a non-gated write left an event: %v", ev)
		}
	}
}
