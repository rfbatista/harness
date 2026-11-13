//go:build !windows

package shim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"operators-mcp/internal/ports"
)

// The worktree read hook of an architect session (ADR 0087, contract "Harness
// server ↔ Agent sessions — architect worktree visibility", "The hook"): claude
// runs it before every tool that reads a path. A path under the worktrees folder
// is allowed only when the worktree is one of the session's own task, which the
// harness answers on every decision, so a delegate started later is covered.
// A hook that cannot decide denies. It always exits 0; a deny is the answer.

const (
	worktreeReadUnreachable = "worktree read hook: the harness could not be reached; try again"
	worktreeReadADR         = "an architect reads only the worktrees of its own task's sessions (ADR 0087)"
)

var worktreeReadClient = &http.Client{Timeout: 10 * time.Second}

// worktreeReadInput is the part of claude's hook input the hook reads.
type worktreeReadInput struct {
	ToolName  string `json:"tool_name"`
	Cwd       string `json:"cwd"`
	ToolInput struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Pattern  string `json:"pattern"`
		Command  string `json:"command"`
	} `json:"tool_input"`
}

// taskWorktreesAnswer is the lookup's answer; Owner is set when the lookup was
// asked about a path that is another task's session's worktree.
type taskWorktreesAnswer struct {
	Root      string `json:"worktrees_root"`
	Worktrees []struct {
		Dir string `json:"dir"`
	} `json:"worktrees"`
	Owner *struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		TaskTitle string `json:"task_title"`
	} `json:"owner"`
}

// worktreeReadHookMain is the hook command: its input from stdin, the folder
// and the lookup URL from the session's environment.
func worktreeReadHookMain() int {
	return worktreeReadMain(os.Stdin, os.Stdout, os.Getenv(ports.WorktreesRootEnv), strings.TrimRight(os.Getenv(ports.TaskWorktreesURLEnv), "/"), worktreeReadClient)
}

// worktreeReadMain decides one tool call. root is the worktrees folder, lookup
// the loopback lookup's URL.
func worktreeReadMain(stdin io.Reader, stdout io.Writer, root, lookup string, client *http.Client) int {
	var in worktreeReadInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil || root == "" {
		return 0
	}
	for _, p := range readPaths(in, root) {
		if reason := judgePath(client, lookup, root, in.ToolName, p); reason != "" {
			reportDenial(client, lookup, in.ToolName, p, reason)
			deny(stdout, reason)
			return 0
		}
	}
	return 0
}

func deny(w io.Writer, reason string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
}

// absPathInText finds the absolute paths of a command's text: a run of
// characters up to a space, a quote or a shell operator.
var absPathInText = regexp.MustCompile("/[^\\s'\"`;|&<>()$\\\\]+")

// readPaths are the paths of the call that lie under root, cleaned. The file
// tools name one path (relative to the working directory when it is not
// absolute); Bash names them anywhere in its command text, absolute ones only.
func readPaths(in worktreeReadInput, root string) []string {
	var candidates []string
	if in.ToolName == "Bash" {
		candidates = absPathInText.FindAllString(in.ToolInput.Command, -1)
	} else {
		for _, p := range []string{in.ToolInput.FilePath, in.ToolInput.Path} {
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(in.Cwd, p)
			}
			candidates = append(candidates, p)
		}
		if filepath.IsAbs(in.ToolInput.Pattern) && in.ToolName == "Glob" {
			candidates = append(candidates, in.ToolInput.Pattern)
		}
	}
	var out []string
	for _, p := range candidates {
		if p = filepath.Clean(p); underFolder(root, p) {
			out = append(out, p)
		}
	}
	return out
}

// underFolder says p is root or lies under it, by name or by where root really
// is (a symlinked temp dir).
func underFolder(root, p string) bool {
	_, ok := belowRoot(root, p)
	return ok
}

// belowRoot is p relative to root, "" for root itself.
func belowRoot(root, p string) (rel string, ok bool) {
	roots := []string{filepath.Clean(root)}
	if real, err := filepath.EvalSymlinks(root); err == nil && real != roots[0] {
		roots = append(roots, real)
	}
	for _, r := range roots {
		if p == r {
			return "", true
		}
		if rest, in := strings.CutPrefix(p, r+string(filepath.Separator)); in {
			return rest, true
		}
	}
	return "", false
}

// firstComponent is the worktree directory name of a path below the folder.
func firstComponent(rel string) string {
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return first
}

// judgePath is the reason the call may not read p, "" when it may.
func judgePath(client *http.Client, lookup, root, tool, p string) string {
	rel, _ := belowRoot(root, p)
	if rel == "" {
		// The folder itself: listing it names worktrees only; a recursive
		// search would read them all.
		if tool == "Grep" || tool == "Glob" {
			return "worktree read hook: " + p + " is not a worktree of this task's sessions (ADR 0087)"
		}
		return ""
	}
	ans, err := askTaskWorktrees(client, lookup, p)
	if err != nil {
		return worktreeReadUnreachable
	}
	comp := firstComponent(rel)
	for _, w := range ans.Worktrees {
		if wrel, ok := belowRoot(root, filepath.Clean(w.Dir)); ok && firstComponent(wrel) == comp {
			return ""
		}
	}
	if o := ans.Owner; o != nil && o.TaskID != "" {
		short := o.SessionID
		if len(short) > 8 {
			short = short[:8]
		}
		return fmt.Sprintf("worktree read hook: %s is the worktree of session %s on task %q (%s); %s", p, short, o.TaskTitle, o.TaskID, worktreeReadADR)
	}
	return "worktree read hook: " + p + " is not a worktree of this task's sessions (ADR 0087)"
}

// askTaskWorktrees is the lookup, asked about path so the answer can name its
// owner when it is another task's.
func askTaskWorktrees(client *http.Client, lookup, path string) (taskWorktreesAnswer, error) {
	var ans taskWorktreesAnswer
	res, err := client.Get(lookup + "?path=" + url.QueryEscape(path))
	if err != nil {
		return ans, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ans, fmt.Errorf("lookup answered %d", res.StatusCode)
	}
	return ans, json.NewDecoder(res.Body).Decode(&ans)
}

// reportDenial tells the harness a call was denied, so it records it with the
// session. A harness that cannot be reached loses the record, not the denial.
func reportDenial(client *http.Client, lookup, tool, path, reason string) {
	body, _ := json.Marshal(map[string]string{"tool": tool, "path": path, "reason": reason})
	res, err := client.Post(lookup+"/denials", "application/json", bytes.NewReader(body))
	if err == nil {
		_ = res.Body.Close()
	}
}
