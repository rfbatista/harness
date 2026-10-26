// Package gitcli shells out to the git binary: worktrees, history, code
// views, env files, publishing branches, and the git directories that give
// every remote operation the harness's stored credentials and nothing of the
// machine's (AuthBroker). Commands that only read a checkout run with
// localOverrides, so they can never authenticate either.
package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Ensure WorktreeManager implements ports.WorktreeManager at compile time.
var _ ports.WorktreeManager = (*WorktreeManager)(nil)

// WorktreeManager creates and removes git worktrees by invoking git. Each
// run is bounded: worktree changes by the worktree deadline, reads by the
// read deadline (see Deadlines).
type WorktreeManager struct{ d config }

// NewWorktreeManager returns a new git worktree manager.
func NewWorktreeManager(opts ...Option) *WorktreeManager {
	return &WorktreeManager{d: newConfig(opts)}
}

// Add creates a worktree at path with a new branch cut from baseRef, which may
// be a local branch, a remote-tracking ref (origin/main), a tag or a SHA. An
// empty baseRef branches from HEAD. An existing branch is an error, never a
// checkout: two worktrees cannot hold the same branch.
//
// path and baseRef sit after a "--" so git's parse-options cannot reinterpret
// either as a flag: without it, a baseRef like "--lock" would permute into an
// option and silently lock the worktree instead of failing as an invalid ref.
//
// It answers the commit the new branch points at, read once the worktree
// exists: a failure to read it leaves it empty rather than undoing the
// worktree, since the commit is only a hint for the merge state.
func (w *WorktreeManager) Add(repoRoot, path, branch, baseRef string) (string, error) {
	exists, err := w.branchExists(repoRoot, branch)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("%w: %s", ports.ErrBranchExists, branch)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	args := []string{"worktree", "add", "-b", branch, "--", path}
	if baseRef != "" {
		args = append(args, baseRef)
	}
	if err := w.run(repoRoot, args...); err != nil {
		return "", err
	}
	sha, _ := w.d.gitOutput(context.Background(), repoRoot, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+branch)
	return strings.TrimSpace(sha), nil
}

// DeleteBranch force-deletes a local branch. Callers must only use this on a
// branch known to carry no work worth keeping (see ports.WorktreeManager).
func (w *WorktreeManager) DeleteBranch(repoRoot, branch string) error {
	return w.run(repoRoot, "branch", "-D", "--", branch)
}

// Remove deletes the worktree at path, discarding any local changes in it.
// If the worktree directory was already deleted out-of-band (e.g. a manual
// `rm -rf` or disk cleanup), `git worktree remove` can no longer find it and
// fails; in that case Remove instead prunes the stale registration so the
// caller can still treat the worktree as gone.
func (w *WorktreeManager) Remove(repoRoot, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return w.run(repoRoot, "worktree", "prune")
	}
	return w.run(repoRoot, "worktree", "remove", "--force", path)
}

// ListBranches reads refs/heads and refs/remotes in one pass. The NUL separator
// keeps branch names with spaces or slashes intact.
func (w *WorktreeManager) ListBranches(repoRoot string) ([]domain.GitBranch, error) {
	out, err := w.d.gitOutput(context.Background(), repoRoot, "for-each-ref",
		"--format=%(refname:short)%00%(refname)%00%(HEAD)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}

	var local, remote []domain.GitBranch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 3 {
			continue
		}
		short, full, head := parts[0], parts[1], parts[2]
		if strings.HasSuffix(full, "/HEAD") {
			continue
		}
		b := domain.GitBranch{
			Name:   short,
			Remote: strings.HasPrefix(full, "refs/remotes/"),
			IsHead: head == "*",
		}
		if b.Remote {
			remote = append(remote, b)
		} else {
			local = append(local, b)
		}
	}
	return append(local, remote...), nil
}

// branchExists reports whether refs/heads/branch exists. A show-ref that
// gives no answer in time is an error, not a missing branch.
func (w *WorktreeManager) branchExists(repoRoot, branch string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), w.d.read())
	defer cancel()
	err := cut(ctx, "show-ref", w.d.gitCmd(ctx, repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run())
	if timedOut(err) {
		return false, err
	}
	return err == nil, nil
}

// run executes git with args in dir under the worktree deadline, wrapping
// stderr into the error so failures are diagnosable from logs and API
// responses.
func (w *WorktreeManager) run(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.d.worktree())
	defer cancel()
	cmd := w.d.gitCmd(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if err := cut(ctx, strings.Join(args, " "), err); timedOut(err) {
			return err
		}
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
