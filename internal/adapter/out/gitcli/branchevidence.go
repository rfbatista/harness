package gitcli

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/ports"
)

var (
	_ ports.GitBranchEvidence = (*Code)(nil)
	_ ports.GitRemoteLister  = (*Code)(nil)
)

// BranchSnapshot reads the repository's refs, remotes and worktrees in three
// git runs, however many branches there are.
func (c Code) BranchSnapshot(ctx context.Context, repoRoot string) (*ports.GitBranchSnapshot, error) {
	snap := &ports.GitBranchSnapshot{Branches: map[string]string{}, RemoteRefs: map[string]string{}}
	refs, err := c.d.codeOutput(ctx, repoRoot, "for-each-ref", "--format=%(objectname) %(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(refs, "\n") {
		oid, ref, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			snap.Branches[strings.TrimPrefix(ref, "refs/heads/")] = oid
		case strings.HasPrefix(ref, "refs/remotes/") && !strings.HasSuffix(ref, "/HEAD"):
			snap.RemoteRefs[strings.TrimPrefix(ref, "refs/remotes/")] = oid
		}
	}
	if snap.Remotes, err = c.Remotes(ctx, repoRoot); err != nil {
		return nil, err
	}
	wts, err := c.d.codeOutput(ctx, repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var cur *ports.GitWorktreeHead
	for _, line := range strings.Split(wts+"\n", "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			snap.Worktrees = append(snap.Worktrees, ports.GitWorktreeHead{Dir: strings.TrimPrefix(line, "worktree ")})
			cur = &snap.Worktrees[len(snap.Worktrees)-1]
		case cur != nil && strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case cur != nil && strings.HasPrefix(line, "branch refs/heads/"):
			cur.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	return snap, nil
}

// Remotes lists the configured remotes with their URLs, in one git run.
func (c Code) Remotes(ctx context.Context, repoRoot string) ([]ports.GitRemote, error) {
	cfg, err := c.d.codeOutput(ctx, repoRoot, "config", "--get-regexp", `^remote\..*\.url$`)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil // config exits 1 when there is no remote
	}
	if err != nil {
		return nil, err
	}
	var out []ports.GitRemote
	for _, line := range strings.Split(cfg, "\n") {
		key, url, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		out = append(out, ports.GitRemote{Name: name, URL: url})
	}
	return out, nil
}

// gitDirs answers dir's own git directory and the common one.
func (c Code) gitDirs(ctx context.Context, dir string) (own, common string, err error) {
	out, err := c.d.codeOutput(ctx, dir, "rev-parse", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return "", "", err
	}
	f := strings.Split(strings.TrimSpace(out), "\n")
	own = f[0]
	common = own
	if len(f) > 1 {
		common = f[1]
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
	}
	return own, common, nil
}

// HEADReflog reads the worktree's own HEAD log file: one git run, no other.
func (c Code) HEADReflog(ctx context.Context, dir string) ([]ports.GitReflogEntry, error) {
	own, _, err := c.gitDirs(ctx, dir)
	if err != nil {
		return nil, err
	}
	entries, err := readReflog(filepath.Join(own, "logs", "HEAD"))
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// RemoteRefUpdates reads the reflog files of the remote-tracking refs.
func (c Code) RemoteRefUpdates(ctx context.Context, repoRoot string) ([]ports.GitRemoteRefUpdate, error) {
	_, common, err := c.gitDirs(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(common, "logs", "refs", "remotes")
	var out []ports.GitRemoteRefUpdate
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		remote, branch, ok := strings.Cut(filepath.ToSlash(rel), "/")
		if !ok || branch == "HEAD" {
			return nil
		}
		entries, err := readReflog(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			out = append(out, ports.GitRemoteRefUpdate{
				Remote: remote, Branch: branch, Old: e.Old, New: e.New, At: e.At,
				Push: strings.HasPrefix(e.Message, "update by push"),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// readReflog reads a reflog file, newest first; a missing file is an empty log.
// A line is "<old> <new> <name> <<email>> <unix> <tz>\t<message>".
func readReflog(path string) ([]ports.GitReflogEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []ports.GitReflogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		head, msg, _ := strings.Cut(sc.Text(), "\t")
		if len(head) < 83 {
			continue
		}
		e := ports.GitReflogEntry{Old: head[:40], New: head[41:81], Message: msg}
		if i := strings.LastIndex(head, "> "); i >= 0 {
			if f := strings.Fields(head[i+2:]); len(f) > 0 {
				if sec, err := strconv.ParseInt(f[0], 10, 64); err == nil {
					e.At = time.Unix(sec, 0)
				}
			}
		}
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, sc.Err()
}
