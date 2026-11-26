package critic

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
)

// GitDiff reads the author's branch with git, in its worktree. BaseSHA gives
// the commit the branch was cut at, by workspace id.
type GitDiff struct {
	BaseSHA func(ctx context.Context, workspaceID string) string
}

var _ DiffSource = GitDiff{}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = dir
	out, err := c.Output()
	return strings.TrimRight(string(out), "\n"), err
}

// Range implements DiffSource.
func (g GitDiff) Range(ctx context.Context, sess *domain.Session) (string, string, error) {
	base := g.BaseSHA(ctx, sess.WorkspaceID)
	if base == "" {
		return "", "", errors.New("the session's worktree has no recorded base commit")
	}
	head, err := git(ctx, sess.WorkingDir, "rev-parse", "HEAD")
	return base, head, err
}

// Diff implements DiffSource: the branch's diff against base, uncommitted
// changes included.
func (g GitDiff) Diff(ctx context.Context, sess *domain.Session, base string) (string, error) {
	return git(ctx, sess.WorkingDir, "diff", base)
}

// File implements DiffSource: a path inside the worktree, nothing outside it.
func (g GitDiff) File(_ context.Context, sess *domain.Session, path string) string {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "..") {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(sess.WorkingDir, path))
	if err != nil {
		return ""
	}
	return string(b)
}
