//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// The edit proxy's hooks as claude runs them: each hook command, run by sh
// with the hook's input on stdin, against the harness (contract "Harness
// server ↔ Agent sessions — virtual shell" §5–6).

// configuredBase is where the e2e config's harness listens; the hook
// commands are aimed at the test server instead.
const configuredBase = "http://127.0.0.1:0"

const editTools = "Edit|Write|MultiEdit|NotebookEdit"

// runHook runs the session's event hook for matcher, as claude would, with
// input on its stdin and the harness at base, and answers its stdout.
func runHook(t *testing.T, s runningSession, base, event, matcher string, input map[string]any) string {
	t.Helper()
	var command string
	for _, g := range s.hooks[event] {
		if g.Matcher == matcher && len(g.Hooks) == 1 {
			command = g.Hooks[0].Command
		}
	}
	if command == "" {
		t.Fatalf("the session has no %s hook on %s: %+v", event, matcher, s.hooks)
	}
	if !strings.Contains(command, configuredBase) {
		t.Fatalf("the %s hook does not call the harness at %s: %s", event, configuredBase, command)
	}
	input["session_id"] = "claude-conversation"
	input["hook_event_name"] = event
	if _, ok := input["cwd"]; !ok {
		input["cwd"] = s.worktree
	}
	stdin, _ := json.Marshal(input)
	cmd := exec.Command("sh", "-c", strings.ReplaceAll(command, configuredBase, base))
	cmd.Dir = s.worktree
	cmd.Env = append(os.Environ(), "HARNESS_SHELL="+s.shell)
	if home, ok := input["home"].(string); ok {
		delete(input, "home")
		stdin, _ = json.Marshal(input)
		cmd.Stdin = bytes.NewReader(stdin)
		cmd.Env = append(cmd.Env, "HOME="+home)
	}
	if cmd.Stdin == nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the %s hook failed: %v\n%s", event, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func editInput(tool, path string) map[string]any {
	return map[string]any{"tool_name": tool, "tool_input": map[string]any{"file_path": path, "old_string": "a", "new_string": "b"}}
}

func bashInput(cwd, command string) map[string]any {
	return map[string]any{"cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": command}}
}

func denied(reason string) string {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
	return string(b)
}

func blocked(reason string) string {
	b, _ := json.Marshal(map[string]any{"decision": "block", "reason": reason})
	return string(b)
}

// sameJSON says a and b are the same JSON value.
func sameJSON(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

// An Edit inside the shell is allowed, then stored; one on a copy the
// person has changed since is refused with BASE_VERSION_STALE after it ran,
// and the file is the stored one afterwards; a read-only file is denied
// before the tool runs; outside the
// shell the hooks say nothing.
func TestTheEditHooksCheckAndCommitEditsInsideTheShell(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell hooks")
	a := e.start(taskA)
	doc := e.createDocument("Plan", "# Plan\n\nv1\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the plan in A", func() error { return hasFile(a.shell, "task/docs/plan.md", "# Plan\n\nv1\n", doc.ID, "task", "") })
	plan := filepath.Join(a.shell, "task", "docs", "plan.md")

	for _, tool := range []string{"Edit", "MultiEdit"} {
		if out := runHook(t, a, e.srv.URL, "PreToolUse", editTools, editInput(tool, plan)); out != "" {
			t.Fatalf("PreToolUse %s inside the shell = %s, want nothing", tool, out)
		}
	}
	agents := "# Plan\n\nv2 by the agent\n"
	writeShellFile(t, a, "task/docs/plan.md", agents)
	if out := runHook(t, a, e.srv.URL, "PostToolUse", editTools, editInput("Edit", plan)); out != "" {
		t.Fatalf("PostToolUse Edit = %s, want nothing", out)
	}
	if d := e.document(doc.ID); d.Content != agents {
		t.Fatalf("the edit was not stored: %q", d.Content)
	}

	// The person saves the page between the check and the commit.
	if out := runHook(t, a, e.srv.URL, "PreToolUse", editTools, editInput("Write", plan)); out != "" {
		t.Fatalf("PreToolUse Write = %s", out)
	}
	person := "# Plan\n\nv3 by the person\n"
	e.post("update_document", map[string]string{"document_id": doc.ID, "title": "Plan", "content": person}, nil)
	writeShellFile(t, a, "task/docs/plan.md", "# Plan\n\nv3 by the agent\n")
	now := domain.DocumentVersion(person)
	stale := "BASE_VERSION_STALE: task/docs/plan.md changed since this session last received it (now " + now + "); the file has been refreshed — read it again and redo the change"
	if out := runHook(t, a, e.srv.URL, "PostToolUse", editTools, editInput("Write", plan)); !sameJSON(out, blocked(stale)) {
		t.Fatalf("PostToolUse on a stale copy = %s, want %s", out, blocked(stale))
	}
	if err := hasFile(a.shell, "task/docs/plan.md", person, doc.ID, "task", now); err != nil {
		t.Fatalf("the shell's file after the refused commit: %v", err)
	}

	// A notebook is named by notebook_path.
	nb := map[string]any{"tool_name": "NotebookEdit", "tool_input": map[string]any{"notebook_path": filepath.Join(a.shell, "README.md")}}
	readOnly := "READ_ONLY: README.md is read-only (task.json, resources, index, README, attached.md, urls.md)"
	if out := runHook(t, a, e.srv.URL, "PreToolUse", editTools, nb); !sameJSON(out, denied(readOnly)) {
		t.Fatalf("PreToolUse NotebookEdit on README.md = %s, want %s", out, denied(readOnly))
	}

	// Outside the shell, absolute or relative to the session's cwd.
	for _, p := range []string{filepath.Join(a.worktree, "main.go"), "main.go"} {
		for _, event := range []string{"PreToolUse", "PostToolUse"} {
			if out := runHook(t, a, e.srv.URL, event, editTools, editInput("Write", p)); out != "" {
				t.Errorf("%s Write %s outside the shell = %s, want nothing", event, p, out)
			}
		}
	}
}

// A commit the harness refuses after the tool already wrote puts the shell
// back as the harness holds it: a changed read-only file gets its stored
// content again, a file it does not store is removed.
func TestARefusedCommitPutsTheShellBack(t *testing.T) {
	e := newShellE2E(t)
	a := e.start(e.task("Shell hooks"))
	taskJSON := filepath.Join(a.shell, "task", "task.json")
	stored, err := os.ReadFile(taskJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(taskJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	writeShellFile(t, a, "task/task.json", `{"title":"rewritten"}`)
	readOnly := "READ_ONLY: task/task.json is read-only (task.json, resources, index, README, attached.md, urls.md)"
	if out := runHook(t, a, e.srv.URL, "PostToolUse", editTools, editInput("Write", taskJSON)); !sameJSON(out, blocked(readOnly)) {
		t.Fatalf("PostToolUse on task.json = %s, want %s", out, blocked(readOnly))
	}
	if got, _ := os.ReadFile(taskJSON); !bytes.Equal(got, stored) {
		t.Fatalf("task.json after the refused commit = %s, want %s", got, stored)
	}

	stray := filepath.Join(a.shell, "task", "stray.md")
	writeShellFile(t, a, "task/stray.md", "# Stray\n")
	out := runHook(t, a, e.srv.URL, "PostToolUse", editTools, editInput("Write", stray))
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "NOT_STORED: task/stray.md") {
		t.Fatalf("PostToolUse on a file the harness does not store = %s", out)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("the unstored file is still in the shell: %v", err)
	}
}

// Bash redirects (>, >>, &>, n>, >|) into the shell are denied with
// REDIRECT_DENIED, wherever the command names the shell from; tee, redirects
// elsewhere and text that only looks like a redirect pass.
func TestTheBashHookDeniesRedirectsIntoTheShell(t *testing.T) {
	e := newShellE2E(t)
	a := e.start(e.task("Shell hooks"))
	const redirect = "REDIRECT_DENIED: shell redirects into $HARNESS_SHELL are not stored; use Write/Edit or tee"

	for _, c := range []struct{ cwd, command string }{
		{a.worktree, `echo hi > "$HARNESS_SHELL/task/docs/notes.md"`},
		{a.worktree, `echo hi >>${HARNESS_SHELL}/task/docs/notes.md`},
		{a.worktree, `make 2> ` + a.shell + `/project/docs/build.log`},
		{a.worktree, `ls &> $HARNESS_SHELL/listing.txt`},
		{a.worktree, `date >| $HARNESS_SHELL/task/docs/when.md`},
		{a.shell, `printf 'x' > task/docs/x.md`},
		{a.worktree, `cd "$HARNESS_SHELL" && echo hi > task/docs/x.md`},
		{a.worktree, "true; echo hi \\\n  > $HARNESS_SHELL/task/docs/x.md"},
	} {
		if out := runHook(t, a, e.srv.URL, "PreToolUse", "Bash", bashInput(c.cwd, c.command)); !sameJSON(out, denied(redirect)) {
			t.Errorf("Bash %q in %s = %s, want %s", c.command, c.cwd, out, denied(redirect))
		}
	}

	// ~ and $HOME are read as the session's home, here the shell's parent.
	home := filepath.Dir(a.shell)
	rel := filepath.Base(a.shell)
	for _, command := range []string{
		`echo hi > ~/` + rel + `/task/docs/x.md`,
		`echo hi > $HOME/` + rel + `/task/docs/x.md`,
		`echo hi > "${HOME}/` + rel + `/task/docs/x.md"`,
	} {
		in := bashInput(a.worktree, command)
		in["home"] = home
		if out := runHook(t, a, e.srv.URL, "PreToolUse", "Bash", in); !sameJSON(out, denied(redirect)) {
			t.Errorf("Bash %q with HOME %s = %s, want %s", command, home, out, denied(redirect))
		}
	}
	for _, command := range []string{
		`echo hi > '~/` + rel + `/task/docs/x.md'`,
		`echo hi > ~/elsewhere.txt`,
	} {
		in := bashInput(a.worktree, command)
		in["home"] = home
		if out := runHook(t, a, e.srv.URL, "PreToolUse", "Bash", in); out != "" {
			t.Errorf("Bash %q with HOME %s = %s, want nothing", command, home, out)
		}
	}

	for _, c := range []struct{ cwd, command string }{
		{a.worktree, `echo hi | tee $HARNESS_SHELL/task/docs/notes.md`},
		{a.worktree, `echo hi > /dev/null 2>&1`},
		{a.worktree, `go test ./... > out.txt`},
		{a.shell, `cat task/task.json > ` + a.worktree + `/task.json`},
		{a.worktree, `echo '> $HARNESS_SHELL/task/docs/x.md'`},
		{a.worktree, `echo "a > $HARNESS_SHELL/b" | grep a`},
		{a.worktree, "cat <<EOF > notes.txt\na > $HARNESS_SHELL/task/docs/x.md\nEOF"},
		{a.worktree, `grep x < $HARNESS_SHELL/task/task.json`},
	} {
		if out := runHook(t, a, e.srv.URL, "PreToolUse", "Bash", bashInput(c.cwd, c.command)); out != "" {
			t.Errorf("Bash %q in %s = %s, want nothing", c.command, c.cwd, out)
		}
	}
}

// With the harness down, an edit tool is denied before it runs with
// SHELL_UNAVAILABLE, and after it ran a commit blocks naming the file that
// changed on disk unstored, so no write is silently lost; Bash runs, since
// its redirects are never stored anyway.
func TestTheHooksRefuseWithShellUnavailableWhenTheHarnessIsDown(t *testing.T) {
	e := newShellE2E(t)
	a := e.start(e.task("Shell hooks"))
	down := httptest.NewServer(nil)
	down.Close()

	const unavailable = "SHELL_UNAVAILABLE: the harness could not be reached; nothing was written"
	plan := filepath.Join(a.shell, "task", "docs", "plan.md")
	if out := runHook(t, a, down.URL, "PreToolUse", editTools, editInput("Write", plan)); !sameJSON(out, denied(unavailable)) {
		t.Fatalf("PreToolUse with the harness down = %q, want %s", out, denied(unavailable))
	}
	if out := runHook(t, a, down.URL, "PreToolUse", "Bash", bashInput(a.worktree, "echo hi > $HARNESS_SHELL/task/docs/x.md")); out != "" {
		t.Fatalf("PreToolUse Bash with the harness down = %q, want nothing", out)
	}
	unstored := "SHELL_UNAVAILABLE: the harness could not be reached; task/docs/plan.md changed on disk but was not stored — redo the change once the harness is back"
	if out := runHook(t, a, down.URL, "PostToolUse", editTools, editInput("Write", plan)); !sameJSON(out, blocked(unstored)) {
		t.Fatalf("PostToolUse with the harness down = %q, want %s", out, blocked(unstored))
	}
	nb := filepath.Join(a.shell, "task", "docs", "nb.ipynb")
	in := map[string]any{"tool_name": "NotebookEdit", "tool_input": map[string]any{"notebook_path": nb, "new_source": "x"}}
	unstoredNB := "SHELL_UNAVAILABLE: the harness could not be reached; task/docs/nb.ipynb changed on disk but was not stored — redo the change once the harness is back"
	if out := runHook(t, a, down.URL, "PostToolUse", editTools, in); !sameJSON(out, blocked(unstoredNB)) {
		t.Fatalf("PostToolUse NotebookEdit with the harness down = %q, want %s", out, blocked(unstoredNB))
	}
}
