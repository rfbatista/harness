package orchestration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// The architecture commit check (ADR 0033): a pre-commit hook in the
// session's worktree asks the harness whether the staged paths keep to the
// agent's layers. It is installed through the worktree's own git
// configuration, so the repository's tracked files and the person's commits in
// the main checkout are untouched.

// preCommitHook is the hook's script. It sends the staged paths, NUL
// separated (as the hook's own index sees them, so `git commit <paths>` is
// checked too), to the session's commit endpoint: 409 fails the commit with
// the rule's text, 200 prints the warnings. A harness that cannot be reached
// lets the commit go and says so. The repository's own pre-commit hook runs
// after the check.
const preCommitHook = `#!/bin/sh
resp=$(git diff --cached --name-only -z --no-renames | curl -sS -m 330 -w '\n%{http_code}' -X POST --data-binary @- '{{URL}}' 2>/dev/null) || resp='
000'
code=$(printf '%s\n' "$resp" | tail -n 1)
body=$(printf '%s\n' "$resp" | sed '$d')
case "$code" in
200) [ -n "$body" ] && printf '%s\n' "$body" >&2 ;;
409) printf '%s\n' "$body" >&2; exit 1 ;;
*) echo '` + ArchitectureUnreachable + `' >&2 ;;
esac
own='{{OWN}}'
if [ -n "$own" ] && [ -x "$own/pre-commit" ]; then
	exec "$own/pre-commit" "$@"
fi
exit 0
`

// withCommitCheck installs the commit check in the worktree at dir, and
// answers cleanup extended to remove it. Without a guard or an endpoint, or
// when the install fails (a commit then goes unchecked, as the hooks say),
// it answers cleanup as it is.
func (s *Service) withCommitCheck(id, dir string, cleanup func()) func() {
	if s.CommitGuard == nil || s.SessionArchitectureURL == nil || dir == "" {
		return cleanup
	}
	hooks, err := os.MkdirTemp("", "harness-githooks-*")
	if err != nil {
		slog.Warn("make the session's commit check", "session", id, "err", err)
		return cleanup
	}
	own, err := s.CommitGuard.Install(context.TODO(), dir, hooks)
	if err != nil {
		slog.Warn("install the session's commit check", "session", id, "err", err)
		_ = os.RemoveAll(hooks)
		return cleanup
	}
	script := strings.NewReplacer("{{URL}}", s.SessionArchitectureURL(id)+"/commit", "{{OWN}}", strings.ReplaceAll(own, "'", `'\''`)).Replace(preCommitHook)
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(script), 0o755); err != nil {
		slog.Warn("write the session's commit check", "session", id, "err", err)
	}
	chainOtherHooks(own, hooks)
	return func() {
		if cleanup != nil {
			cleanup()
		}
		_ = s.CommitGuard.Remove(context.TODO(), dir)
		_ = os.RemoveAll(hooks)
	}
}

// chainOtherHooks links the repository's other hooks into hooks, so the
// guard's hooks folder leaves every hook but pre-commit as it was.
func chainOtherHooks(own, hooks string) {
	entries, err := os.ReadDir(own)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == "pre-commit" || strings.HasSuffix(name, ".sample") || e.IsDir() {
			continue
		}
		_ = os.Symlink(filepath.Join(own, name), filepath.Join(hooks, name))
	}
}
