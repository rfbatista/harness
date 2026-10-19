package gitcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitCode = (*Code)(nil)

// Code reads code out of a checkout for the code views. Every command it
// runs only reads: no fetch, no ref or index write, no hook. Each is bounded
// by the read deadline.
type Code struct{ d config }

// NewCode returns the adapter.
func NewCode(opts ...Option) *Code { return &Code{d: newConfig(opts)} }

// codeGitArgs come before every subcommand: names are printed as they are,
// a configured fsmonitor command never runs, pathspecs are literal (":(…)"
// in a path is text, not magic).
var codeGitArgs = []string{"-c", "core.quotePath=false", "-c", "core.fsmonitor=false", "--no-pager", "--literal-pathspecs"}

// codeGitEnv keeps git from writing the index back after a refresh
// (GIT_OPTIONAL_LOCKS=0), prompting, or fetching a missing object of a
// partial clone (GIT_NO_LAZY_FETCH, git 2.44+).
var codeGitEnv = []string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_LITERAL_PATHSPECS=1"}

func (d config) codeCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, d.bin(), append(append([]string{}, codeGitArgs...), args...)...)
	cmd.Dir = dir
	cmd.Env = localEnv(codeGitEnv...)
	cmd.WaitDelay = waitDelay
	return cmd
}

// codeOutput is the output of a code read, under the read deadline.
func (d config) codeOutput(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d.read())
	defer cancel()
	cmd := d.codeCmd(ctx, dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if err := cut(ctx, args[0], err); timedOut(err) {
			return "", err
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func refNotFound(ref string) error {
	return &domain.StructuredError{Code: "REF_NOT_FOUND", Message: "ref " + ref + " does not resolve to a commit in this repository"}
}

func pathNotFound(p string) error {
	return &domain.StructuredError{Code: "PATH_NOT_FOUND", Message: p + " is not in this source"}
}

// ResolveCommit resolves ref to the full hash of the commit it names.
func (c Code) ResolveCommit(ctx context.Context, dir, ref string) (string, error) {
	out, err := c.d.codeOutput(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if timedOut(err) {
			return "", err
		}
		return "", refNotFound(ref)
	}
	return strings.TrimSpace(out), nil
}

// CurrentBranch is the branch checked out in dir, "" when detached.
func (c Code) CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := c.d.codeOutput(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if timedOut(err) {
			return "", err
		}
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// MergeBase is the best common ancestor of a and b.
func (c Code) MergeBase(ctx context.Context, dir, a, b string) (string, error) {
	out, err := c.d.codeOutput(ctx, dir, "merge-base", "--end-of-options", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// FirstParent is commit's first parent, "" for a root commit.
func (c Code) FirstParent(ctx context.Context, dir, commit string) (string, error) {
	out, err := c.d.codeOutput(ctx, dir, "rev-list", "--parents", "-n", "1", "--end-of-options", commit, "--")
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	if len(f) < 2 {
		return "", nil
	}
	return f[1], nil
}

// EmptyTree names the empty tree in the repository's hash format. hash-object
// without -w writes nothing.
func (c Code) EmptyTree(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.d.read())
	defer cancel()
	cmd := c.d.codeCmd(ctx, dir, "hash-object", "-t", "tree", "--stdin")
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.Output()
	if err := cut(ctx, "hash-object", err); timedOut(err) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("git hash-object: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// lsTree runs ls-tree -z -l and parses its entries:
// "<mode> <type> <object> <size>\t<path>\0", size "-" for a tree or commit.
func (d config) lsTree(ctx context.Context, dir string, args ...string) ([]ports.GitEntry, error) {
	out, err := d.codeOutput(ctx, dir, append([]string{"ls-tree", "-z", "-l", "--full-tree"}, args...)...)
	if err != nil {
		return nil, err
	}
	var entries []ports.GitEntry
	for rec := range strings.SplitSeq(out, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 4 {
			return nil, fmt.Errorf("git ls-tree: unexpected record %q", rec)
		}
		e := ports.GitEntry{Path: p, Kind: kindOfMode(f[0]), Object: f[2], Size: -1}
		if n, err := strconv.ParseInt(f[3], 10, 64); err == nil && e.Kind == domain.EntryFile {
			e.Size = n
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// kindOfMode maps a git tree or index mode to an entry kind.
func kindOfMode(mode string) string {
	switch mode {
	case "040000":
		return domain.EntryDir
	case "120000":
		return domain.EntrySymlink
	case "160000":
		return domain.EntrySubmodule
	}
	return domain.EntryFile
}

// maxLinkTarget bounds a symlink target read from its blob.
const maxLinkTarget = 4096

// PathKind walks path one directory at a time, so a symlink on the way is
// seen, never followed.
func (c Code) PathKind(ctx context.Context, dir, commit, p string) (ports.GitEntry, error) {
	if p == "" {
		return ports.GitEntry{Kind: domain.EntryDir, Size: -1}, nil
	}
	segs := strings.Split(p, "/")
	for i := range segs {
		sub := strings.Join(segs[:i+1], "/")
		found, err := c.d.lsTree(ctx, dir, "--end-of-options", commit, "--", sub)
		if err != nil {
			return ports.GitEntry{}, err
		}
		if len(found) != 1 || found[0].Path != sub {
			return ports.GitEntry{}, pathNotFound(p)
		}
		e := found[0]
		last := i == len(segs)-1
		switch e.Kind {
		case domain.EntrySymlink:
			target, err := c.Blob(ctx, dir, e.Object, maxLinkTarget)
			if err != nil {
				return ports.GitEntry{}, err
			}
			e.Target = string(target)
			if !last {
				if domain.SymlinkEscapes(sub, e.Target) {
					return ports.GitEntry{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: p + " goes through a symlink out of the repository"}
				}
				return ports.GitEntry{}, pathNotFound(p)
			}
			return e, nil
		case domain.EntryDir:
			if last {
				return e, nil
			}
		default:
			if !last {
				return ports.GitEntry{}, pathNotFound(p)
			}
			return e, nil
		}
	}
	return ports.GitEntry{}, pathNotFound(p)
}

// TreeAt lists the directory p at commit: its direct children with their
// symlinks' targets, or every file under it when recursive.
func (c Code) TreeAt(ctx context.Context, dir, commit, p string, recursive bool) ([]ports.GitEntry, error) {
	if p != "" {
		e, err := c.PathKind(ctx, dir, commit, p)
		if err != nil {
			return nil, err
		}
		if e.Kind == domain.EntrySymlink && domain.SymlinkEscapes(p, e.Target) {
			return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: p + " is a symlink out of the repository"}
		}
		if e.Kind != domain.EntryDir {
			return nil, pathNotFound(p)
		}
	}
	args := []string{}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, "--end-of-options", commit)
	if p != "" {
		args = append(args, "--", p+"/")
	}
	entries, err := c.d.lsTree(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	if !recursive {
		var links []string
		for _, e := range entries {
			if e.Kind == domain.EntrySymlink {
				links = append(links, e.Object)
			}
		}
		targets, err := c.d.blobs(ctx, dir, links, maxLinkTarget)
		if err != nil {
			return nil, err
		}
		for i, e := range entries {
			if e.Kind == domain.EntrySymlink {
				entries[i].Target = targets[e.Object]
			}
		}
	}
	return entries, nil
}

// blobs reads small blobs in one git cat-file --batch, each cut at max
// bytes: "<oid> blob <size>\n<content>\n" per object asked.
func (d config) blobs(ctx context.Context, dir string, objects []string, max int) (map[string]string, error) {
	out := map[string]string{}
	if len(objects) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, d.read())
	defer cancel()
	cmd := d.codeCmd(ctx, dir, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(objects, "\n") + "\n")
	raw, err := cmd.Output()
	if err := cut(ctx, "cat-file", err); timedOut(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("git cat-file --batch: %w", err)
	}
	for _, object := range objects {
		header, rest, ok := bytes.Cut(raw, []byte("\n"))
		if !ok {
			return nil, fmt.Errorf("git cat-file --batch: short output")
		}
		f := strings.Fields(string(header))
		if len(f) != 3 {
			return nil, fmt.Errorf("git cat-file --batch: %q", header)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil || size+1 > len(rest) {
			return nil, fmt.Errorf("git cat-file --batch: %q", header)
		}
		out[object] = string(rest[:min(size, max)])
		raw = rest[size+1:]
	}
	return out, nil
}

// WorktreeEntries lists the index (one entry per path, conflicts merged),
// with deleted marking what is gone from disk, and the untracked files git
// does not ignore.
func (c Code) WorktreeEntries(ctx context.Context, dir string, deleted bool) ([]ports.GitEntry, error) {
	staged, err := c.d.codeOutput(ctx, dir, "ls-files", "-z", "-s", "--full-name")
	if err != nil {
		return nil, err
	}
	var entries []ports.GitEntry
	seen := map[string]bool{}
	for rec := range strings.SplitSeq(staged, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		if !ok || seen[p] {
			continue
		}
		seen[p] = true
		mode, _, _ := strings.Cut(meta, " ")
		entries = append(entries, ports.GitEntry{Path: p, Kind: kindOfMode(mode), Size: -1})
	}
	if deleted {
		out, err := c.d.codeOutput(ctx, dir, "ls-files", "-z", "-d", "--full-name")
		if err != nil {
			return nil, err
		}
		gone := map[string]bool{}
		for p := range strings.SplitSeq(out, "\x00") {
			gone[p] = p != ""
		}
		for i := range entries {
			entries[i].Deleted = gone[entries[i].Path]
		}
	}
	others, err := c.UntrackedFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	for _, p := range others {
		if !seen[p] {
			entries = append(entries, ports.GitEntry{Path: p, Kind: domain.EntryFile, Size: -1, Untracked: true})
		}
	}
	return entries, nil
}

// UntrackedFiles lists the untracked files git does not ignore.
func (c Code) UntrackedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := c.d.codeOutput(ctx, dir, "ls-files", "-z", "-o", "--exclude-standard", "--full-name")
	if err != nil {
		return nil, err
	}
	var paths []string
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// Blob reads at most max bytes of the blob, stopping git once it has them.
func (c Code) Blob(ctx context.Context, dir, object string, max int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.d.read())
	defer cancel()
	cmd := c.d.codeCmd(ctx, dir, "cat-file", "blob", object)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(out, max))
	full := int64(len(data)) < max
	if !full {
		cancel() // the rest is not wanted
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if err := cut(ctx, "cat-file", waitErr); timedOut(err) {
		return nil, err
	}
	if full && waitErr != nil {
		return nil, fmt.Errorf("git cat-file %s: %w: %s", object, waitErr, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

// Snapshot fingerprints a worktree: HEAD, git status, and the size and
// modification time of every path status lists, since status alone does
// not change when an already modified file is edited again.
func (c Code) Snapshot(ctx context.Context, dir string) (string, string, error) {
	head, err := c.d.codeOutput(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil && ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if timedOut(err) {
		return "", "", err
	}
	head = strings.TrimSpace(head)
	status, err := c.d.codeOutput(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00", head, status)
	recs := strings.Split(status, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 4 {
			continue
		}
		if rec[0] == 'R' || rec[0] == 'C' {
			i++ // the original path follows
		}
		if info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rec[3:]))); err == nil {
			fmt.Fprintf(h, "%s %d %d\x00", rec[3:], info.Size(), info.ModTime().UnixNano())
		}
	}
	return head, hex.EncodeToString(h.Sum(nil)), nil
}
