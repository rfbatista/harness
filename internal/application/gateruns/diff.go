package gateruns

import (
	"context"
	"os/exec"
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
