//go:build !windows

package shim

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The worktree read hook of an architect session (contract "Harness server ↔
// Agent sessions — architect worktree visibility", "The hook").

const hookRoot = "/ws/harness-r1"

// lookupServer stands for the harness: the task's worktrees, and the owner of
// a path under the folder that is another task's. It records the denials it
// is told about and counts the lookups.
type lookupServer struct {
	*httptest.Server
	mu      sync.Mutex
	lookups int
	denials []map[string]string
	// mine are the worktree dirs of the architect's task.
	mine []string
}

func newLookupServer(t *testing.T, mine ...string) *lookupServer {
	t.Helper()
	l := &lookupServer{mine: mine}
	l.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if r.Method == http.MethodPost {
			var d map[string]string
			_ = json.NewDecoder(r.Body).Decode(&d)
			l.denials = append(l.denials, d)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		l.lookups++
		var wts []map[string]string
		for i, dir := range l.mine {
			wts = append(wts, map[string]string{"session_id": "s" + string(rune('a'+i)), "dir": dir, "branch": "b", "agent": "dev", "status": "running"})
		}
		out := map[string]any{"worktrees_root": hookRoot, "worktrees": wts}
		if p := r.URL.Query().Get("path"); strings.HasPrefix(p, hookRoot+"/other-task") {
			out["owner"] = map[string]string{"session_id": "0123456789abcdef", "task_id": "tk9", "task_title": "Other work"}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(l.Close)
	return l
}

func (l *lookupServer) hook(t *testing.T, input string) (code int, out string) {
	t.Helper()
	var sb strings.Builder
	code = worktreeReadMain(strings.NewReader(input), &sb, hookRoot, l.URL, &http.Client{})
	return code, sb.String()
}

func toolInput(tool, field, value string) string {
	b, _ := json.Marshal(map[string]any{"tool_name": tool, "cwd": hookRoot + "/mine", "tool_input": map[string]string{field: value}})
	return string(b)
}

func denied(out string) (reason string, ok bool) {
	var v struct {
		Hook struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal([]byte(out), &v) != nil || v.Hook.Decision != "deny" || v.Hook.Event != "PreToolUse" {
		return "", false
	}
	return v.Hook.Reason, true
}

func TestWorktreeReadHook_ADeniedPathNamesTheOtherTasksSessionAndTask(t *testing.T) {
	l := newLookupServer(t, hookRoot+"/mine")
	for tool, field := range map[string]string{"Read": "file_path", "Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "Grep": "path", "Glob": "path", "LS": "path"} {
		p := hookRoot + "/other-task/src/main.go"
		code, out := l.hook(t, toolInput(tool, field, p))
		reason, ok := denied(out)
		want := `worktree read hook: ` + p + ` is the worktree of session 01234567 on task "Other work" (tk9); an architect reads only the worktrees of its own task's sessions (ADR 0087)`
		if code != 0 || !ok || reason != want {
			t.Errorf("%s: code %d, out %q, want the deny %q", tool, code, out, want)
		}
	}
}

func TestWorktreeReadHook_APathOfNoKnownSessionIsDeniedAsNotTheTasks(t *testing.T) {
	l := newLookupServer(t, hookRoot+"/mine")
	p := hookRoot + "/stranger/x"
	_, out := l.hook(t, toolInput("Read", "file_path", p))
	if reason, ok := denied(out); !ok || reason != "worktree read hook: "+p+" is not a worktree of this task's sessions (ADR 0087)" {
		t.Errorf("out = %q", out)
	}
}

func TestWorktreeReadHook_ATaskSessionsWorktreeIsAllowedWhenTheServerListsItNow(t *testing.T) {
	l := newLookupServer(t, hookRoot+"/mine")
	if _, out := l.hook(t, toolInput("Read", "file_path", hookRoot+"/later/a.go")); out == "" {
		t.Fatal("a worktree the server does not list is allowed")
	}
	// The delegate starts after the architect did; the server lists it at once.
	l.mu.Lock()
	l.mine = append(l.mine, hookRoot+"/later")
	l.mu.Unlock()
	for _, p := range []string{hookRoot + "/later/a.go", hookRoot + "/later", hookRoot + "/mine/b/c.go"} {
		if code, out := l.hook(t, toolInput("Read", "file_path", p)); code != 0 || out != "" {
			t.Errorf("%s: code %d out %q, want allowed with no output", p, code, out)
		}
	}
}

func TestWorktreeReadHook_APathOutsideTheFolderIsNeverTouchedAndAsksNothing(t *testing.T) {
	l := newLookupServer(t)
	for _, in := range []string{
		toolInput("Read", "file_path", "/etc/hosts"),
		toolInput("Read", "file_path", "/ws/harness-r1-other/x"),
		toolInput("Bash", "command", "ls /tmp && cat /ws/elsewhere/file"),
		toolInput("Bash", "command", "echo hi"),
	} {
		if code, out := l.hook(t, in); code != 0 || out != "" {
			t.Errorf("%s: code %d out %q", in, code, out)
		}
	}
	if l.lookups != 0 {
		t.Errorf("%d lookups for paths outside the folder", l.lookups)
	}
}

func TestWorktreeReadHook_BashIsCheckedByEveryAbsolutePathInTheCommandText(t *testing.T) {
	l := newLookupServer(t, hookRoot+"/mine")
	cmd := `cat ` + hookRoot + `/mine/ok.txt && grep -r "x" '` + hookRoot + `/other-task/src' | head`
	_, out := l.hook(t, toolInput("Bash", "command", cmd))
	reason, ok := denied(out)
	if !ok || !strings.Contains(reason, hookRoot+"/other-task/src is the worktree of session 01234567") {
		t.Errorf("out = %q", out)
	}
	if _, out := l.hook(t, toolInput("Bash", "command", "cat "+hookRoot+"/mine/ok.txt; ls "+hookRoot+"/mine")); out != "" {
		t.Errorf("a command naming only the task's worktrees is allowed: %q", out)
	}
}

func TestWorktreeReadHook_ARelativePathIsJudgedFromTheWorkingDirectory(t *testing.T) {
	l := newLookupServer(t, hookRoot+"/mine")
	_, out := l.hook(t, toolInput("Read", "file_path", "../stranger/x.go"))
	if _, ok := denied(out); !ok {
		t.Errorf("a path that climbs out of the own worktree into another is denied: %q", out)
	}
	if _, out := l.hook(t, toolInput("Read", "file_path", "src/x.go")); out != "" {
		t.Errorf("a path inside the own worktree is allowed: %q", out)
	}
}

func TestWorktreeReadHook_AnUnreachableHarnessDeniesAPathUnderTheFolderOnly(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	url := down.URL
	down.Close()
	run := func(in string) string {
		var sb strings.Builder
		if code := worktreeReadMain(strings.NewReader(in), &sb, hookRoot, url, &http.Client{}); code != 0 {
			t.Errorf("exit code %d", code)
		}
		return sb.String()
	}
	reason, ok := denied(run(toolInput("Read", "file_path", hookRoot+"/mine/a.go")))
	if !ok || reason != "worktree read hook: the harness could not be reached; try again" {
		t.Errorf("reason = %q", reason)
	}
	if out := run(toolInput("Read", "file_path", "/etc/hosts")); out != "" {
		t.Errorf("a path outside the folder needs no harness: %q", out)
	}
}

func TestWorktreeReadHook_EachDenialIsReportedToTheHarness(t *testing.T) {
	l := newLookupServer(t, hookRoot+"/mine")
	p := hookRoot + "/other-task/a.go"
	l.hook(t, toolInput("Grep", "path", p))
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.denials) != 1 || l.denials[0]["tool"] != "Grep" || l.denials[0]["path"] != p || !strings.Contains(l.denials[0]["reason"], "ADR 0087") {
		t.Errorf("denials = %v", l.denials)
	}
}

func TestWorktreeReadHook_AGrepOrGlobOverTheWholeFolderIsDeniedAndAReadOfItsListingIsNot(t *testing.T) {
	l := newLookupServer(t, hookRoot+"/mine")
	if _, out := l.hook(t, toolInput("Grep", "path", hookRoot)); out == "" {
		t.Error("a recursive search over every worktree is allowed")
	}
	if _, out := l.hook(t, toolInput("LS", "path", hookRoot)); out != "" {
		t.Errorf("listing the folder is allowed: %q", out)
	}
}

func TestWorktreeReadHook_ACallItCannotJudgeIsDenied(t *testing.T) {
	var out strings.Builder
	worktreeReadMain(strings.NewReader("not json"), &out, hookRoot, "http://127.0.0.1:1", &http.Client{})
	if _, ok := denied(out.String()); !ok {
		t.Errorf("garbled input: %q", out.String())
	}
	out.Reset()
	worktreeReadMain(strings.NewReader(toolInput("Read", "file_path", "/etc/hosts")), &out, "", "http://127.0.0.1:1", &http.Client{})
	if _, ok := denied(out.String()); !ok {
		t.Errorf("no folder: %q", out.String())
	}
}
