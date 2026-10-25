package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.ArchitectureGit = (*Architecture)(nil)

// Architecture reads a checkout's default branch for the repository
// architecture: its file list and the files it keeps. Every run only reads,
// and is bounded by the read deadline.
type Architecture struct{ d config }

// NewArchitecture returns the adapter.
func NewArchitecture(opts ...Option) *Architecture { return &Architecture{d: newConfig(opts)} }

// DefaultBranch is the branch origin's HEAD names when the checkout has it,
// else main, else master, else the branch checked out; "" when it has none.
func (a Architecture) DefaultBranch(ctx context.Context, dir string) (string, error) {
	out, err := a.d.gitOutput(ctx, dir, "for-each-ref", "--format=%(refname) %(symref)", "refs/heads/", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	heads, originHead := map[string]bool{}, ""
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if name, ok := strings.CutPrefix(f[0], "refs/heads/"); ok {
			heads[name] = true
		} else if len(f) == 2 {
			originHead = strings.TrimPrefix(f[1], "refs/remotes/origin/")
		}
	}
	for _, name := range []string{originHead, "main", "master"} {
		if name != "" && heads[name] {
			return name, nil
		}
	}
	cur, err := a.d.gitOutput(ctx, dir, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(cur), nil
}

// Files lists every file the branch tracks, sorted.
func (a Architecture) Files(ctx context.Context, dir, branch string) ([]string, error) {
	out, err := a.d.gitOutput(ctx, dir, "ls-tree", "-r", "-z", "--name-only", "--full-tree", "--end-of-options", "refs/heads/"+branch)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// ReadFile reads path as the branch has it; false when the branch has no such
// file.
func (a Architecture) ReadFile(ctx context.Context, dir, branch, path string) ([]byte, bool, error) {
	spec := "refs/heads/" + branch + ":" + path
	ok, err := a.d.gitOK(ctx, dir, "cat-file", "-e", spec)
	if err != nil || !ok {
		return nil, false, err
	}
	rctx, cancel := context.WithTimeout(ctx, a.d.read())
	defer cancel()
	cmd := a.d.gitCmd(rctx, dir, "cat-file", "blob", spec)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, false, fmt.Errorf("git cat-file: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), true, nil
}
