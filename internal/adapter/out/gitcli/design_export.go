package gitcli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.DesignExportGit = (*Architecture)(nil)

// ReadTree reads every file the branch tracks under prefix ("" for all),
// keyed by its path relative to prefix, with two git runs however many files
// there are.
func (a Architecture) ReadTree(ctx context.Context, dir, branch, prefix string) (map[string][]byte, error) {
	args := []string{"ls-tree", "-r", "-z", "--end-of-options", "refs/heads/" + branch}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	out, err := a.d.gitOutput(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	var paths, shas []string
	for _, rec := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta) // mode type sha
		if len(f) != 3 || f[1] != "blob" {
			continue
		}
		rel, ok := strings.CutPrefix(path, prefix)
		if !ok {
			continue
		}
		paths, shas = append(paths, rel), append(shas, f[2])
	}
	files := make(map[string][]byte, len(paths))
	if len(paths) == 0 {
		return files, nil
	}
	rctx, cancel := context.WithTimeout(ctx, a.d.read())
	defer cancel()
	cmd := a.d.gitCmd(rctx, dir, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	r := bufio.NewReader(&stdout)
	for _, p := range paths {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("git cat-file: %w", err)
		}
		f := strings.Fields(header) // sha type size
		if len(f) != 3 {
			return nil, fmt.Errorf("git cat-file: unexpected %q", strings.TrimSpace(header))
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fmt.Errorf("git cat-file: %w", err)
		}
		body := make([]byte, size+1) // the batch output ends each object with a newline
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, fmt.Errorf("git cat-file: %w", err)
		}
		files[p] = body[:size]
	}
	return files, nil
}

// CommitChanges writes files, deletes removed (paths the checkout tracks)
// and commits just those paths.
func (a Architecture) CommitChanges(ctx context.Context, dir string, files map[string][]byte, removed []string, message string) (head string, err error) {
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	paths = append(paths, removed...)
	sort.Strings(paths)
	before := map[string][]byte{}
	for _, p := range paths {
		if b, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
			before[p] = b
		}
	}
	undo := func() {
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.d.worktree())
		defer cancel()
		_, _ = a.d.gitOutput(uctx, dir, append([]string{"reset", "-q", "--"}, paths...)...)
		for _, p := range paths {
			full := filepath.Join(dir, p)
			if b, ok := before[p]; ok {
				_ = os.MkdirAll(filepath.Dir(full), 0o755)
				_ = os.WriteFile(full, b, 0o644)
			} else {
				_ = os.Remove(full)
			}
		}
		pruneEmptyDirs(dir, paths)
	}
	for _, p := range removed {
		if err := os.Remove(filepath.Join(dir, p)); err != nil && !os.IsNotExist(err) {
			undo()
			return "", err
		}
	}
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			undo()
			return "", err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			undo()
			return "", err
		}
	}
	pruneEmptyDirs(dir, removed)
	cctx, cancel := context.WithTimeout(ctx, a.d.worktree())
	defer cancel()
	if _, err := a.d.gitRun(cctx, dir, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		undo()
		return "", err
	}
	args := append(append([]string{}, harnessIdentity...), "commit", "-q", "--no-verify", "-m", message, "--")
	if _, err := a.d.gitRun(cctx, dir, append(args, paths...)...); err != nil {
		undo()
		return "", err
	}
	out, err := a.d.gitOutput(ctx, dir, "rev-parse", "--verify", "HEAD")
	return strings.TrimSpace(out), err
}

// pruneEmptyDirs removes the folders of paths that the deletions emptied,
// innermost first, never the checkout itself.
func pruneEmptyDirs(dir string, paths []string) {
	for _, p := range paths {
		for d := filepath.Dir(p); d != "." && d != "/"; d = filepath.Dir(d) {
			if os.Remove(filepath.Join(dir, d)) != nil {
				break
			}
		}
	}
}
