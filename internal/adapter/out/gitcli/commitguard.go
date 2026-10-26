package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.CommitGuard = (*CommitGuard)(nil)

// CommitGuard sets a worktree's core.hooksPath in its own config file
// (extensions.worktreeConfig), so the main checkout and the repository's
// tracked files are untouched.
type CommitGuard struct{ d config }

// NewCommitGuard returns the adapter.
func NewCommitGuard(opts ...Option) *CommitGuard { return &CommitGuard{d: newConfig(opts)} }

// Install implements ports.CommitGuard.
func (g CommitGuard) Install(ctx context.Context, worktree, hooksDir string) (string, error) {
	// A guard left by an earlier start of the session is lifted first, so the
	// hooks to chain to are the repository's own.
	_ = g.Remove(ctx, worktree)
	previous, err := g.hooksOf(ctx, worktree)
	if err != nil {
		return "", err
	}
	// extensions.worktreeConfig lets each worktree carry its own config; it is
	// set once, in the repository's shared config.
	if _, err := g.d.gitOutput(ctx, worktree, "config", "extensions.worktreeConfig", "true"); err != nil {
		return "", err
	}
	if _, err := g.d.gitOutput(ctx, worktree, "config", "--worktree", "core.hooksPath", hooksDir); err != nil {
		return "", err
	}
	return previous, nil
}

// hooksOf is the directory the worktree's hooks are in now: core.hooksPath
// when set, else the repository's hooks folder.
func (g CommitGuard) hooksOf(ctx context.Context, worktree string) (string, error) {
	if out, err := g.d.gitOutput(ctx, worktree, "config", "--path", "--get", "core.hooksPath"); err == nil {
		if p := strings.TrimSpace(out); p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(worktree, p)
			}
			return p, nil
		}
	}
	out, err := g.d.gitOutput(ctx, worktree, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Remove implements ports.CommitGuard.
func (g CommitGuard) Remove(ctx context.Context, worktree string) error {
	if _, err := os.Stat(worktree); err != nil {
		return nil
	}
	_, _ = g.d.gitOutput(ctx, worktree, "config", "--worktree", "--unset", "core.hooksPath")
	return nil
}
