// Package gitcli shells out to the git binary for worktree management.
package gitcli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Ensure WorktreeManager implements ports.WorktreeManager at compile time.
var _ ports.WorktreeManager = (*WorktreeManager)(nil)

// WorktreeManager creates and removes git worktrees by invoking git.
type WorktreeManager struct{}

// NewWorktreeManager returns a new git worktree manager.
func NewWorktreeManager() *WorktreeManager { return &WorktreeManager{} }

// Add creates a worktree at path with a new branch cut from baseRef, which may
// be a local branch, a remote-tracking ref (origin/main), a tag or a SHA. An
// empty baseRef branches from HEAD. An existing branch is an error, never a
// checkout: two worktrees cannot hold the same branch.
//
// path and baseRef sit after a "--" so git's parse-options cannot reinterpret
// either as a flag: without it, a baseRef like "--lock" would permute into an
// option and silently lock the worktree instead of failing as an invalid ref.
func (w *WorktreeManager) Add(repoRoot, path, branch, baseRef string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if w.branchExists(repoRoot, branch) {
		return fmt.Errorf("%w: %s", ports.ErrBranchExists, branch)
	}
	args := []string{"worktree", "add", "-b", branch, "--", path}
	if baseRef != "" {
		args = append(args, baseRef)
	}
	return run(repoRoot, args...)
}

// DeleteBranch force-deletes a local branch. Callers must only use this on a
// branch known to carry no work worth keeping (see ports.WorktreeManager).
func (w *WorktreeManager) DeleteBranch(repoRoot, branch string) error {
	return run(repoRoot, "branch", "-D", "--", branch)
}

// Remove deletes the worktree at path, discarding any local changes in it.
// If the worktree directory was already deleted out-of-band (e.g. a manual
// `rm -rf` or disk cleanup), `git worktree remove` can no longer find it and
// fails; in that case Remove instead prunes the stale registration so the
// caller can still treat the worktree as gone.
func (w *WorktreeManager) Remove(repoRoot, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return run(repoRoot, "worktree", "prune")
	}
	return run(repoRoot, "worktree", "remove", "--force", path)
}

// ListBranches reads refs/heads and refs/remotes in one pass. The NUL separator
// keeps branch names with spaces or slashes intact.
func (w *WorktreeManager) ListBranches(repoRoot string) ([]domain.GitBranch, error) {
	cmd := exec.Command("git", "for-each-ref",
		"--format=%(refname:short)%00%(refname)%00%(HEAD)",
		"refs/heads", "refs/remotes")
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var local, remote []domain.GitBranch
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
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

func (w *WorktreeManager) branchExists(repoRoot, branch string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repoRoot
	return cmd.Run() == nil
}

// run executes git with args in dir, wrapping stderr into the error so
// failures are diagnosable from logs and API responses.
func run(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
