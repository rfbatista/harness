package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra/readshare"
	"operators-mcp/internal/ports"
)

var _ ports.GitHistory = (*History)(nil)

// History reads a checkout's commit graph with git log and git show. Each
// git run is bounded by the read deadline.
type History struct{ d Deadlines }

// NewHistory returns the adapter.
func NewHistory(opts ...Option) *History { return &History{d: newDeadlines(opts)} }

const (
	defaultHistory = 300
	unitSep        = "\x1f"
	recordSep      = "\x1e"
	// commitFormat: hash, parents, author, email, date, decorations, subject.
	commitFormat = "%H%x1f%P%x1f%an%x1f%ae%x1f%aI%x1f%D%x1f%s%x1e"
)

// Log lists commits children-first (--topo-order), so a graph can be laid out
// row by row. Decorations are read fully qualified to tell branches, remote
// branches and tags apart.
func (h History) Log(ctx context.Context, repoRoot string, q ports.HistoryQuery) ([]domain.Commit, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultHistory
	}
	limit = min(limit, domain.MaxHistory)
	args := []string{"log", "--topo-order", "--decorate=full", "--format=" + commitFormat, "-n", strconv.Itoa(limit)}
	var out string
	var err error
	if len(q.Refs) > 0 {
		out, err = h.logRefs(ctx, repoRoot, args, q.Refs)
	} else {
		args = append(args, "--branches", "--tags")
		if q.Remotes {
			args = append(args, "--remotes")
		}
		// HEAD too, for a detached checkout; an empty repository has none.
		hasHead, headErr := h.d.gitOK(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "HEAD")
		if headErr != nil {
			return nil, headErr
		}
		if hasHead {
			args = append(args, "HEAD")
		}
		out, err = h.d.gitOutput(ctx, repoRoot, append(args, "--")...)
	}
	if err != nil {
		return nil, err
	}
	var commits []domain.Commit
	for _, rec := range strings.Split(out, recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		c, err := parseCommit(rec)
		if err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// logRefs runs the log args over refs only, leaving out those that do not
// resolve; "" when none does. Refs that all resolve, the usual case, take one
// git run; only a log that fails checks them one by one.
func (h History) logRefs(ctx context.Context, repoRoot string, args, refs []string) (string, error) {
	// --end-of-options keeps a ref from reading as an option.
	args = append(args, "--end-of-options")
	out, err := h.d.gitOutput(ctx, repoRoot, append(append(args, refs...), "--")...)
	if err == nil || timedOut(err) {
		return out, err
	}
	resolved := 0
	for _, ref := range refs {
		ok, err := h.d.gitOK(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
		if err != nil {
			return "", err
		}
		if ok {
			args = append(args, ref)
			resolved++
		}
	}
	if resolved == 0 {
		return "", nil // none of them resolves
	}
	return h.d.gitOutput(ctx, repoRoot, append(args, "--")...)
}

// Show reads one commit's full message and the files it changed, against
// its first parent for a merge.
func (h History) Show(ctx context.Context, repoRoot, hash string) (*domain.CommitDetail, error) {
	if !domain.ValidCommitHash(hash) {
		return nil, &domain.StructuredError{Code: "COMMIT_NOT_FOUND", Message: "commit " + hash + " not found in this repository"}
	}
	if ok, err := h.d.gitOK(ctx, repoRoot, "cat-file", "-e", hash+"^{commit}"); err != nil {
		return nil, err
	} else if !ok {
		return nil, &domain.StructuredError{Code: "COMMIT_NOT_FOUND", Message: "commit " + hash + " not found in this repository"}
	}
	out, err := h.d.gitOutput(ctx, repoRoot, "show", "--no-patch", "--decorate=full", "--format="+strings.TrimSuffix(commitFormat, "%x1e")+"%x1f%b", hash, "--")
	if err != nil {
		return nil, err
	}
	fields := strings.SplitN(strings.TrimRight(out, "\n"), unitSep, 8)
	if len(fields) < 8 {
		return nil, fmt.Errorf("git show %s: unexpected output", hash)
	}
	c, err := parseCommit(strings.Join(fields[:7], unitSep))
	if err != nil {
		return nil, err
	}
	stat, err := h.d.gitOutput(ctx, repoRoot, "show", "--numstat", "--format=", "--diff-merges=first-parent", hash, "--")
	if err != nil {
		return nil, err
	}
	detail := &domain.CommitDetail{Commit: c, Body: strings.TrimSpace(fields[7]), Files: []domain.FileChange{}}
	for _, line := range strings.Split(strings.TrimSpace(stat), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		detail.Files = append(detail.Files, domain.FileChange{Path: parts[2], Added: numstat(parts[0]), Deleted: numstat(parts[1])})
	}
	return detail, nil
}

func parseCommit(rec string) (domain.Commit, error) {
	f := strings.SplitN(rec, unitSep, 7)
	if len(f) != 7 {
		return domain.Commit{}, fmt.Errorf("git log: unexpected record %q", rec)
	}
	at, err := time.Parse(time.RFC3339, f[4])
	if err != nil {
		return domain.Commit{}, fmt.Errorf("git log: date %q: %w", f[4], err)
	}
	c := domain.Commit{
		Hash: f[0], Parents: strings.Fields(f[1]), AuthorName: f[2], AuthorEmail: f[3],
		AuthoredAt: at, Subject: f[6], Refs: parseRefs(f[5]),
	}
	if c.Parents == nil {
		c.Parents = []string{}
	}
	return c, nil
}

// parseRefs reads git's full decorations: "HEAD -> refs/heads/main,
// refs/remotes/origin/main, tag: refs/tags/v1".
func parseRefs(d string) []domain.GitRef {
	var refs []domain.GitRef
	for _, part := range strings.Split(d, ", ") {
		part = strings.TrimSpace(part)
		head := false
		if rest, ok := strings.CutPrefix(part, "HEAD -> "); ok {
			part, head = rest, true
		}
		part = strings.TrimPrefix(part, "tag: ")
		switch {
		case part == "":
		case part == "HEAD":
			refs = append(refs, domain.GitRef{Name: "HEAD", Kind: domain.RefHead})
		case strings.HasPrefix(part, "refs/heads/"):
			refs = append(refs, domain.GitRef{Name: strings.TrimPrefix(part, "refs/heads/"), Kind: domain.RefBranch, Head: head})
		case strings.HasPrefix(part, "refs/remotes/"):
			if !strings.HasSuffix(part, "/HEAD") {
				refs = append(refs, domain.GitRef{Name: strings.TrimPrefix(part, "refs/remotes/"), Kind: domain.RefRemote})
			}
		case strings.HasPrefix(part, "refs/tags/"):
			refs = append(refs, domain.GitRef{Name: strings.TrimPrefix(part, "refs/tags/"), Kind: domain.RefTag})
		}
	}
	return refs
}

func numstat(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1 // "-": a binary file
	}
	return n
}

// gitOutput is the output of git args in dir, under the read deadline.
func (d Deadlines) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d.read())
	defer cancel()
	cmd := gitCmd(ctx, dir, args...)
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

// gitOK reports whether git args in dir succeeds, under the read deadline.
// Only a run that ends with ctx (or the deadline) is an error: a git that
// answers no is false.
func (d Deadlines) gitOK(ctx context.Context, dir string, args ...string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, d.read())
	defer cancel()
	err := gitCmd(ctx, dir, args...).Run()
	if err != nil && ctx.Err() != nil {
		return false, cut(ctx, args[0], ctx.Err())
	}
	return err == nil, nil
}

// Status reads where a checkout is: HEAD, its branch, and every uncommitted
// change (staged, unstaged, untracked) with its line counts against HEAD.
// Within a unit of work that shares its reads (a page load), a checkout's
// status is read once for all who ask, and must not be changed.
func (h History) Status(ctx context.Context, dir string) (*domain.WorktreeStatus, error) {
	return readshare.Do(ctx, "git status\x00"+dir, func() (*domain.WorktreeStatus, error) { return h.status(ctx, dir) })
}

func (h History) status(ctx context.Context, dir string) (*domain.WorktreeStatus, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "the worktree " + dir + " is no longer on disk"}
	}
	// The four reads are independent, so they run side by side: a page that
	// shows a worktree waits for the slowest git run, not for their sum.
	var head, branch, porcelain, stat gitResult
	var wg sync.WaitGroup
	for _, run := range []struct {
		out  *gitResult
		args []string
	}{
		{&head, []string{"rev-parse", "--verify", "--quiet", "HEAD"}},
		{&branch, []string{"symbolic-ref", "--quiet", "--short", "HEAD"}},
		{&porcelain, []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}},
		{&stat, []string{"diff", "HEAD", "--numstat", "-z", "--"}},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run.out.text, run.out.err = h.d.gitOutput(ctx, dir, run.args...)
		}()
	}
	wg.Wait()

	st := &domain.WorktreeStatus{Changes: []domain.WorkingChange{}}
	// An empty repository has no HEAD and a detached one no branch: only a
	// git that gave no answer in time fails the status.
	if head.err == nil {
		st.Head = strings.TrimSpace(head.text)
	} else if timedOut(head.err) {
		return nil, head.err
	}
	if branch.err == nil {
		st.Branch = strings.TrimSpace(branch.text)
	} else if timedOut(branch.err) {
		return nil, branch.err
	}
	if porcelain.err != nil {
		return nil, porcelain.err
	}
	counts := map[string][2]int{}
	if st.Head != "" { // with no HEAD there is nothing to diff against, and the diff failed
		if stat.err != nil {
			return nil, stat.err
		}
		counts = parseNumstatZ(stat.text)
	}
	entries := strings.Split(porcelain.text, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		code, path := e[:2], e[3:]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // the original path follows a rename or copy
		}
		ch := domain.WorkingChange{Path: path, Status: code, Added: -1, Deleted: -1}
		if n, ok := counts[path]; ok {
			ch.Added, ch.Deleted = n[0], n[1]
		}
		st.Changes = append(st.Changes, ch)
	}
	return st, nil
}

// gitResult is one git run's output or error.
type gitResult struct {
	text string
	err  error
}

// parseNumstatZ reads `git diff --numstat -z`: "added\tdeleted\tpath\0", or for
// a rename "added\tdeleted\t\0old\0new\0". Binary files ("-") are -1.
func parseNumstatZ(out string) map[string][2]int {
	counts := map[string][2]int{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) {
			path = fields[i+2] // rename: old, then new
			i += 2
		}
		counts[path] = [2]int{numstat(parts[0]), numstat(parts[1])}
	}
	return counts
}

// Divergence counts commits on either side of base...branch.
func (h History) Divergence(ctx context.Context, dir, base, branch string) (int, int, error) {
	out, err := h.d.gitOutput(ctx, dir, "rev-list", "--left-right", "--count", "--end-of-options", base+"..."+branch, "--")
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("git rev-list: unexpected output %q", out)
	}
	behind, _ := strconv.Atoi(f[0])
	ahead, _ := strconv.Atoi(f[1])
	return ahead, behind, nil
}
