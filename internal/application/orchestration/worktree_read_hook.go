package orchestration

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/ports"
)

// The architect's worktree read hook (ADR 0087, contract "Harness server ↔
// Agent sessions — architect worktree visibility", "The hook").

// WorktreeReadTools are the tools that read, or read before they write, a path.
const WorktreeReadTools = "Read|Grep|Glob|LS|Bash|Edit|Write|MultiEdit"

// worktreeReadTimeout is how long claude lets the hook run: its lookup and
// the report of a denial each wait ten seconds.
const worktreeReadTimeout = 25

// worktreeReadUnreachable is what the agent is told when the hook itself could
// not run: a hook that cannot decide denies.
const worktreeReadUnreachable = "worktree read hook: the harness could not be reached; try again"

// WorktreeReadHooks are the claude settings hooks of an architect session: the
// shim's command in the session's shim dir, which denies by claude's PreToolUse
// answer. If the command cannot run at all the fallback denies the same way.
func WorktreeReadHooks() map[claude.HookEvent][]claude.HookMatcher {
	down := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"` + worktreeReadUnreachable + `"}}`
	command := ports.WorktreeReadHookCommand + " || printf '%s\\n' '" + down + "'"
	return map[claude.HookEvent][]claude.HookMatcher{
		claude.HookPreToolUse: {{Matcher: WorktreeReadTools, Hooks: []claude.Hook{{Type: "command", Command: command, Timeout: worktreeReadTimeout}}}},
	}
}

// withWorktreeReadHook gives a sandboxed architect session the hook that
// narrows its grant of the worktrees folder to its task, and the environment
// the hook reads: the folder and the lookup. Every other session, a session
// with no sandbox, and a server with no shim binary or lookup URL change
// nothing: there is no grant to narrow.
func (s *Service) withWorktreeReadHook(cfg *llmkit.SessionConfig, id string, sb sandboxed) {
	if sb.worktreesDir == "" || s.SessionTaskWorktreesURL == nil || s.ShimBinary == "" {
		return
	}
	// The command is one more link in the session's shim dir, which its cleanup
	// removes; a session with no shim dir has no way to run it.
	dir := sessionEnvValue(cfg.Env, ports.ShimDirEnv)
	if dir == "" {
		return
	}
	dc, ok := withHooks(cfg.Driver, WorktreeReadHooks())
	if !ok {
		return
	}
	if err := os.Symlink(s.ShimBinary, filepath.Join(dir, ports.WorktreeReadHookCommand)); err != nil && !errors.Is(err, fs.ErrExist) {
		return
	}
	cfg.Driver = dc
	cfg.Env = append(slices.Clone(cfg.Env), ports.WorktreesRootEnv+"="+sb.worktreesDir, ports.TaskWorktreesURLEnv+"="+s.SessionTaskWorktreesURL(id))
}

// sessionEnvValue is the last value env gives name, "" when it gives none.
func sessionEnvValue(env []string, name string) string {
	value := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			value = v
		}
	}
	return value
}
