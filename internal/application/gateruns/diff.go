package gateruns

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
)

// GitHasDiff answers whether a session's worktree differs from the commit its
// branch was cut at: uncommitted changes, or commits since. baseSHA gives the
// commit by workspace id; an unknown base counts as no diff.
func GitHasDiff(baseSHA func(ctx context.Context, workspaceID string) string) func(context.Context, *domain.Session) bool {
	return func(ctx context.Context, sess *domain.Session) bool {
		git := func(args ...string) string {
			c := exec.CommandContext(ctx, "git", args...)
			c.Dir = sess.WorkingDir
			out, err := c.Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		}
		if git("status", "--porcelain") != "" {
			return true
		}
		base := baseSHA(ctx, sess.WorkspaceID)
		if base == "" {
			return false
		}
		n := git("rev-list", "--count", base+"..HEAD")
		return n != "" && n != "0"
	}
}

// GitPathChanged answers whether path (relative to the worktree) differs from
// the commit the session's branch was cut at: uncommitted, or changed by commits
// since. An unknown base counts the uncommitted changes alone.
func GitPathChanged(baseSHA func(ctx context.Context, workspaceID string) string) func(context.Context, *domain.Session, string) bool {
	return func(ctx context.Context, sess *domain.Session, rel string) bool {
		git := func(args ...string) string {
			c := exec.CommandContext(ctx, "git", args...)
			c.Dir = sess.WorkingDir
			out, err := c.Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		}
		if git("status", "--porcelain", "--", rel) != "" {
			return true
		}
		base := baseSHA(ctx, sess.WorkspaceID)
		return base != "" && git("diff", "--name-only", base, "HEAD", "--", rel) != ""
	}
}

// GitDiffOf answers the diff of path (relative to the worktree) against HEAD,
// the whole file as added when it is not tracked yet.
func GitDiffOf() func(context.Context, *domain.Session, string) string {
	return func(ctx context.Context, sess *domain.Session, rel string) string {
		run := func(args ...string) (string, error) {
			c := exec.CommandContext(ctx, "git", args...)
			c.Dir = sess.WorkingDir
			out, err := c.Output()
			return string(out), err
		}
		if _, err := run("ls-files", "--error-unmatch", "--", rel); err != nil {
			b, rerr := os.ReadFile(filepath.Join(sess.WorkingDir, rel))
			if rerr != nil {
				return ""
			}
			return "+" + strings.ReplaceAll(string(b), "\n", "\n+")
		}
		out, _ := run("diff", "HEAD", "--", rel)
		return out
	}
}
