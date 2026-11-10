package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"operators-mcp/internal/ports"
)

var _ ports.ServiceBranchGit = (*ServiceBranches)(nil)

// ServiceBranches is the git a command service's branch runs need (ADR 0080):
// listing a repository's branches, and keeping the service worktree the
// harness owns at a branch's tip.
type ServiceBranches struct{ d config }

// NewServiceBranches returns the adapter.
func NewServiceBranches(opts ...Option) *ServiceBranches {
	return &ServiceBranches{d: newConfig(opts)}
}

// ListServiceBranches lists local branches then origin's, each group newest
// commit first. The NUL separator keeps names with spaces intact.
func (s *ServiceBranches) ListServiceBranches(ctx context.Context, repoRoot string) ([]ports.ServiceBranch, error) {
	out, err := s.d.gitOutput(ctx, repoRoot, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%00%(objectname:short)%00%(contents:subject)%00%(committerdate:iso-strict)",
		"refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	var local, remote []ports.ServiceBranch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 4 {
			continue
		}
		b := ports.ServiceBranch{SHA: parts[1], Subject: parts[2]}
		if at, err := time.Parse(time.RFC3339, parts[3]); err == nil {
			b.CommittedAt = at.UTC().Format(time.RFC3339)
		}
		switch {
		case strings.HasPrefix(parts[0], "refs/heads/"):
			b.Name = strings.TrimPrefix(parts[0], "refs/heads/")
			local = append(local, b)
		case strings.HasPrefix(parts[0], "refs/remotes/origin/"):
			b.Name = strings.TrimPrefix(parts[0], "refs/remotes/origin/")
			if b.Name == "HEAD" {
				continue
			}
			b.Remote = true
			remote = append(remote, b)
		}
	}
	return append(local, remote...), nil
}

// ResolveCommit is the full sha ref names.
func (s *ServiceBranches) ResolveCommit(ctx context.Context, repoRoot, ref string) (string, error) {
	out, err := s.d.gitOutput(ctx, repoRoot, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// run is a git change under the worktree deadline.
func (s *ServiceBranches) run(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.d.worktree())
	defer cancel()
	return s.d.gitRun(ctx, dir, args...)
}

// PrepareServiceWorktree adds the worktree at dir, detached, unless it is
// already a worktree of the repository. A leftover directory that is not one
// (an interrupted add, a copy) is cleared first: the module owns the path.
func (s *ServiceBranches) PrepareServiceWorktree(ctx context.Context, repoRoot, dir string) error {
	if s.isWorktreeOf(ctx, repoRoot, dir) {
		return nil
	}
	if _, err := s.run(ctx, repoRoot, "worktree", "prune"); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	_, err := s.run(ctx, repoRoot, "worktree", "add", "--detach", "--", dir, "HEAD")
	return err
}

// isWorktreeOf says dir is a registered worktree of the repository at root.
func (s *ServiceBranches) isWorktreeOf(ctx context.Context, repoRoot, dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, ".git")); err != nil || fi.IsDir() {
		return false
	}
	want, err := s.commonDir(ctx, repoRoot)
	if err != nil {
		return false
	}
	got, err := s.commonDir(ctx, dir)
	return err == nil && got == want
}

// commonDir is the absolute git directory the checkout at dir shares with its
// repository's other worktrees.
func (s *ServiceBranches) commonDir(ctx context.Context, dir string) (string, error) {
	out, err := s.d.gitOutput(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(strings.TrimSpace(out))
}

// CheckoutDetached moves the worktree to sha with its tracked files reset.
// There is no clean: untracked and ignored files stay.
func (s *ServiceBranches) CheckoutDetached(ctx context.Context, dir, sha string) error {
	if _, err := s.run(ctx, dir, "checkout", "--force", "--detach", "--end-of-options", sha); err != nil {
		return err
	}
	_, err := s.run(ctx, dir, "reset", "--hard", "--quiet", "--end-of-options", sha)
	return err
}

// RemoveServiceWorktree deregisters the worktree through its repository's
// common git directory, so it works from outside the repository and after its
// checkout is gone, then deletes what is left of the directory.
func (s *ServiceBranches) RemoveServiceWorktree(ctx context.Context, dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if common, err := s.commonDir(ctx, dir); err == nil {
		// A failure is left to the fallback below: the registration is then
		// pruned by the repository's next worktree prune.
		_, _ = s.run(ctx, "", "--git-dir="+common, "worktree", "remove", "--force", "--", dir)
	}
	return os.RemoveAll(dir)
}
