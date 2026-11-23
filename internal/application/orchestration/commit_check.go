package orchestration

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The architecture commit check (ADR 0033): a pre-commit hook in the
// session's worktree asks the harness whether the staged paths keep to the
// agent's layers. It is installed through the worktree's own git
// configuration, so the repository's tracked files and the person's commits in
// the main checkout are untouched.

// preCommitHook is the hook's script, shared by every session on the
// worktree (ADR 0105). It sends the staged paths, NUL separated (as the hook's
// own index sees them, so `git commit <paths>` is checked too), to the
// committing session's own commit endpoint, which git's environment names; a
// commit made with no session environment goes to the worktree's endpoint,
// which attributes it. 409 fails the commit with
// the rule's text, 200 prints the warnings. A harness that cannot be reached
// lets the commit go and says so. The repository's own pre-commit hook runs
// after the check.
const preCommitHook = `#!/bin/sh
base="${HARNESS_ARCHITECTURE_URL:-{{FALLBACK}}}"
resp=$(git diff --cached --name-only -z --no-renames | curl -sS -m 330 -w '\n%{http_code}' -X POST --data-binary @- "$base/commit" 2>/dev/null) || resp='
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

// withCommitCheck guards the worktree at dir for a session being started or
// resumed, and answers cleanup as it was: the hooks dir outlives the session's
// process (ADR 0040), so a stop, a failure or a server shutdown leaves it in
// place and only RemoveCommitCheck takes it away. A guard that cannot be put
// in leaves the commit unchecked, as the hooks say; the session is then
// marked "Commit check off" and its agent is told not to bypass hooks. A
// resume first looks at the guard it finds, so a folder lost while the session
// was stopped is reported.
func (s *Service) withCommitCheck(id, workspaceID, dir string, cfg *llmkit.SessionConfig, resume bool, cleanup func()) func() {
	_ = s.EnsureCommitCheck(id, workspaceID, dir, resume)
	if reason := s.commitCheckReason(id); reason != "" {
		cfg.AppendSystem = appendSection(cfg.AppendSystem, commitCheckOffSection(reason))
	}
	return cleanup
}

// commitCheckOffSection tells the agent that its commits are not checked.
func commitCheckOffSection(reason string) string {
	return "## Commit check off\n\nThe harness's commit check is off in your worktree: " + reason + ". " +
		"Your commits run no architecture check. Do not bypass hooks (no `-c core.hooksPath=/dev/null`, no `--no-verify`). " +
		"Tell the person the check is off and carry on; do not work around it."
}

// commitCheckEnabled tells whether the service guards commits at all.
func (s *Service) commitCheckEnabled() bool {
	return s.CommitGuard != nil && s.SessionArchitectureURL != nil && s.CommitCheckDir != nil
}

// EnsureCommitCheck guards the worktree at dir and keeps the session's commit
// check state: with probe, the guard found there is looked at first (a missing
// folder or hook, or none at all, is a session going off, announced before the
// repair); then the hooks dir is rewritten and the worktree pointed at it. The
// state clears when that succeeds and stays off, with the error, when it
// fails. A service with no guard installs nothing. The hooks dir is the
// worktree's, named after workspaceID, and shared by every session on it
// (ADR 0105); the state is the session's own.
func (s *Service) EnsureCommitCheck(id, workspaceID, dir string, probe bool) error {
	if !s.commitCheckEnabled() || dir == "" || workspaceID == "" {
		return nil
	}
	found := ""
	if probe {
		found = s.commitCheckProblem(dir)
	}
	if err := s.InstallCommitCheck(id, workspaceID, dir); err != nil {
		slog.Warn("install the session's commit check", "session", id, "err", err)
		reason := "the repair failed: " + err.Error()
		if found != "" {
			reason = found + ", and " + reason
		}
		s.setCommitCheck(id, reason)
		return err
	}
	if reason := s.commitCheckProblem(dir); reason != "" {
		s.setCommitCheck(id, reason)
		return nil
	}
	if found != "" {
		// It was off until this repair: say so, then that it is guarded again.
		s.setCommitCheck(id, found)
	}
	s.setCommitCheck(id, "")
	return nil
}

// commitCheckProblem says why the worktree at dir is not guarded now, or ""
// when its hooks path names the session's readable hook.
func (s *Service) commitCheckProblem(dir string) string {
	path, err := s.CommitGuard.HooksPath(context.TODO(), dir)
	switch {
	case err != nil:
		return "its hooks path cannot be read: " + err.Error()
	case path == "":
		return "the worktree has no commit check hooks path"
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "the hooks folder " + path + " is missing"
		}
		return "the hooks folder " + path + " cannot be read: " + err.Error()
	}
	hook := filepath.Join(path, "pre-commit")
	f, err := os.Open(hook)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "the commit check hook " + hook + " is missing"
		}
		return "the commit check hook " + hook + " cannot be read: " + err.Error()
	}
	_ = f.Close()
	return ""
}

// setCommitCheck records why the session is unguarded ("" once it is guarded
// again), puts the change on the session feed and announces it. While the
// session stays off, the announcement repeats at each check; a subscriber
// deduplicates it.
func (s *Service) setCommitCheck(id, reason string) {
	s.mu.Lock()
	was := s.commitOff[id]
	if reason == "" {
		delete(s.commitOff, id)
	} else {
		s.commitOff[id] = reason
	}
	s.mu.Unlock()
	if reason == "" && was == "" {
		return
	}
	s.announceCommitCheck(id, was != reason)
}

// announceCommitCheck publishes the session's commit check state, and puts the
// session on the feed when changed. A session not yet recorded (a start in
// progress) is announced once it is.
func (s *Service) announceCommitCheck(id string, changed bool) {
	sess := s.sessions.Get(id)
	if sess == nil {
		return
	}
	reason := s.commitCheckReason(id)
	s.announce(domain.SessionCommitCheckChanged{SessionID: id, ProjectID: sess.ProjectID, TicketID: sess.TicketID, Off: reason != "", Reason: reason})
	if changed {
		s.notify(id, "")
	}
}

// commitCheckReason is why the session is unguarded, "" when it is guarded.
func (s *Service) commitCheckReason(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitOff[id]
}

// InstallCommitCheck (re)writes the worktree's commit check hooks dir in place
// and points the worktree at dir to it. It runs at every start, resume and
// server start, by whichever session is on the worktree, so it is safe to run
// again over its own earlier result. The hook does not depend on the session
// that installs it: the committing session names itself at commit time, and
// id only supplies the fallback endpoint where no worktree endpoint is set. A
// service without a guard, an endpoint or a hooks location installs nothing.
func (s *Service) InstallCommitCheck(id, workspaceID, dir string) error {
	if !s.commitCheckEnabled() || dir == "" || workspaceID == "" {
		return nil
	}
	hooks := s.CommitCheckDir(workspaceID)
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return fmt.Errorf("make the commit check hooks dir: %w", err)
	}
	// Install lifts the guard first, so own is the repository's own hooks and
	// never this folder.
	own, err := s.CommitGuard.Install(context.TODO(), dir, hooks)
	if err != nil {
		return fmt.Errorf("point the worktree at the commit check hooks dir: %w", err)
	}
	fallback := s.SessionArchitectureURL(id)
	if s.WorktreeArchitectureURL != nil {
		fallback = s.WorktreeArchitectureURL(workspaceID)
	}
	script := strings.NewReplacer("{{FALLBACK}}", fallback, "{{OWN}}", strings.ReplaceAll(own, "'", `'\''`)).Replace(preCommitHook)
	if err := writeFileAtomically(filepath.Join(hooks, "pre-commit"), []byte(script), 0o755); err != nil {
		return fmt.Errorf("write the commit check: %w", err)
	}
	chainOtherHooks(own, hooks)
	return nil
}

// RemoveCommitCheck ends the session's part in the commit check for good. The
// worktree's hooks path goes back to the repository's own and the hooks dir is
// deleted only when no other session is recorded on the worktree: the check
// belongs to the worktree, so the end or archive of one session never strips
// it from the others (ADR 0105).
func (s *Service) RemoveCommitCheck(id, workspaceID, dir string) {
	if !s.commitCheckEnabled() {
		return
	}
	s.mu.Lock()
	delete(s.commitOff, id)
	s.mu.Unlock()
	if workspaceID == "" || len(s.sessionsOn(workspaceID, id)) > 0 {
		return
	}
	if dir != "" {
		_ = s.CommitGuard.Remove(context.TODO(), dir)
	}
	_ = os.RemoveAll(s.CommitCheckDir(workspaceID))
}

// sessionsOn is the sessions recorded on a worktree other than except, oldest
// first.
func (s *Service) sessionsOn(workspaceID, except string) []*domain.Session {
	var out []*domain.Session
	for _, o := range s.sessions.List(context.Background(), ports.SessionFilter{WorkspaceID: workspaceID}) {
		if o.ID != except {
			out = append(out, o)
		}
	}
	slices.SortStableFunc(out, func(a, b *domain.Session) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

// CommitAttribution is who a commit made on the worktree with no session
// environment (the person's own terminal) is checked as, and it never lets one
// pass unchecked: the session that cut the worktree, whatever its state, while
// it is recorded; otherwise the newest session with write access; otherwise
// the newest session at all (a read-only one refuses the commit); and with no
// session on the worktree, the worktree itself, as a plain agent. Only a
// worktree no one knows is refused.
func (s *Service) CommitAttribution(ctx context.Context, workspaceID string) (ports.CommitAttribution, error) {
	sessions := s.sessionsOn(workspaceID, "")
	pick := func(of func(*domain.Session) bool) string {
		for i := len(sessions) - 1; i >= 0; i-- { // newest first
			if of(sessions[i]) {
				return sessions[i].ID
			}
		}
		return ""
	}
	// The creator is the oldest session that did not join.
	for _, o := range sessions {
		if !o.Joined {
			return ports.CommitAttribution{SessionID: o.ID}, nil
		}
	}
	if id := pick(func(o *domain.Session) bool { return o.Access.Writes() }); id != "" {
		return ports.CommitAttribution{SessionID: id}, nil
	}
	if id := pick(func(*domain.Session) bool { return true }); id != "" {
		return ports.CommitAttribution{SessionID: id}, nil
	}
	lookup, _ := s.workspaces.(worktreeLookup)
	if lookup == nil {
		return ports.CommitAttribution{}, &domain.StructuredError{Code: "WORKTREE_NOT_FOUND", Message: "no such worktree"}
	}
	ws := lookup.Get(ctx, workspaceID)
	if ws == nil {
		return ports.CommitAttribution{}, &domain.StructuredError{Code: "WORKTREE_NOT_FOUND", Message: "no such worktree"}
	}
	out := ports.CommitAttribution{RepositoryID: ws.RepositoryID, Dir: ws.Path}
	if repo, err := s.catalog.Repositories.GetRepository(ctx, ws.RepositoryID); err == nil && repo != nil {
		out.ProjectID = repo.ProjectID
	}
	return out, nil
}

// repairParallelism bounds the git calls a boot repair makes at once.
const repairParallelism = 8

// RepairCommitChecks is the boot pass (ADR 0040): for every session that still
// has a worktree, whatever runs it, it rewrites the commit check hooks dir and
// points the worktree at it, which also moves worktrees off an old temporary
// folder. A session whose repair fails does not stop the others; it is
// answered in failed, by session id. A worktree is repaired once, however many
// sessions are on it. Hooks dirs of worktrees no session has any more are
// removed, and so are the per-session dirs of the layout before ADR 0105.
func (s *Service) RepairCommitChecks(ctx context.Context) (map[string]error, error) {
	if !s.commitCheckEnabled() {
		return nil, nil
	}
	all, err := s.sessions.Query(ctx, ports.SessionFilter{})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		gate = make(chan struct{}, repairParallelism)
		live = map[string]bool{}
		bad  = map[string]error{}
	)
	// Sessions are grouped by worktree: one repair per worktree, whose outcome
	// every session on it shares.
	groups := map[string][]*domain.Session{}
	var order []string
	for _, sess := range all {
		// Not a session worktree: left as it is, never the main checkout. A
		// worktree that is gone is not repaired; the sweep removes its hooks dir.
		if sess.WorkspaceID == "" || sess.WorkingDir == "" {
			continue
		}
		if _, err := os.Stat(sess.WorkingDir); err != nil {
			continue
		}
		live[sess.WorkspaceID] = true
		// The worktree may still point at the old dir named after the session
		// (ADR 0040): it stays until the worktree is re-pointed.
		live[sess.ID] = true
		if _, ok := groups[sess.WorkspaceID]; !ok {
			order = append(order, sess.WorkspaceID)
		}
		groups[sess.WorkspaceID] = append(groups[sess.WorkspaceID], sess)
	}
	for _, ws := range order {
		group := groups[ws]
		wg.Add(1)
		gate <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-gate }()
			first := group[0]
			err := s.EnsureCommitCheck(first.ID, ws, first.WorkingDir, true)
			reason := s.commitCheckReason(first.ID)
			for _, other := range group[1:] {
				s.setCommitCheck(other.ID, reason)
			}
			if err != nil {
				mu.Lock()
				for _, sess := range group {
					bad[sess.ID] = err
				}
				mu.Unlock()
				return
			}
			// Pointed at the worktree's dir now: the old per-session dirs go.
			for _, sess := range group {
				if sess.ID != ws {
					_ = os.RemoveAll(s.CommitCheckDir(sess.ID))
				}
			}
		}()
	}
	wg.Wait()
	s.sweepCommitCheckDirs(live)
	return bad, nil
}

// sweepCommitCheckDirs removes the hooks dirs of worktrees no session has any
// more, and every folder of the old per-session layout (their names are not
// workspace ids, so they are never live).
func (s *Service) sweepCommitCheckDirs(live map[string]bool) {
	// Only a root laid out as <root>/<session id> is swept: any other layout
	// could make a sibling folder look like a stale hooks dir.
	probe := s.CommitCheckDir("x")
	if filepath.Base(probe) != "x" {
		return
	}
	root := filepath.Dir(probe)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && !live[e.Name()] {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// writeFileAtomically replaces path with body in one rename, so a commit that
// runs while the hook is rewritten sees the old or the new one, never half.
func writeFileAtomically(path string, body []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pre-commit-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// chainOtherHooks links the repository's other hooks into hooks, so the
// guard's hooks folder leaves every hook but pre-commit as it was.
func chainOtherHooks(own, hooks string) {
	if old, err := os.ReadDir(hooks); err == nil {
		for _, e := range old {
			if e.Name() != "pre-commit" && e.Type()&os.ModeSymlink != 0 {
				_ = os.Remove(filepath.Join(hooks, e.Name()))
			}
		}
	}
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

// withReadOnlyAccess tells a read-only session what it is, and lets its git
// read a worktree it cannot write: GIT_OPTIONAL_LOCKS=0 stops status, log and
// diff from trying to refresh the index, which takes a lock in the read-only
// git dir. A session that may write is left as it is.
func withReadOnlyAccess(cfg *llmkit.SessionConfig, access domain.SessionAccess, branch string) {
	if access.Writes() {
		return
	}
	cfg.Env = append(cfg.Env, "GIT_OPTIONAL_LOCKS=0")
	cfg.AppendSystem = appendSection(cfg.AppendSystem, readOnlySection(branch))
}

// readOnlySection is the brief's section for a session with read-only access.
func readOnlySection(branch string) string {
	return "## Read-only access\n\nThis session has read-only access to its worktree, on branch " + branch + ". " +
		"You can read files and run builds and tests there. You cannot write or delete a file in the worktree, stage, commit or change the branch: " +
		"the sandbox refuses it, and an \"Operation not permitted\" from a write is that refusal, not a fault to work around. " +
		"Another session shares this worktree and may be writing in it. Report what you find in your reply; if a change is needed, say so and leave it to a session with write access."
}
