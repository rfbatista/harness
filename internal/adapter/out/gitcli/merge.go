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

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitMerger = (*Merger)(nil)

// Merger merges a branch into the branch a checkout has checked out. It is
// the only thing in the harness that writes a main checkout, and it does so
// as the person runs it: the checkout's own git config, hooks and committer.
// It spawns git as few times as it can: a merge is a status, a count, the
// merge and a read of the new HEAD. It never pushes, and every run is bounded:
// reads by the read deadline, the merge and its abort by the worktree one.
type Merger struct{ d config }

// NewMerger returns the adapter.
func NewMerger(opts ...Option) *Merger { return &Merger{d: newConfig(opts)} }

// HasBranch reports whether refs/heads/<branch> resolves.
func (m Merger) HasBranch(ctx context.Context, dir, branch string) bool {
	ok, _ := m.d.gitOK(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", "refs/heads/"+branch+"^{commit}")
	return ok
}

// Checkout reads the checkout with one git status.
func (m Merger) Checkout(ctx context.Context, dir string) (ports.CheckoutState, error) {
	st, _, err := m.status(ctx, dir)
	return st, err
}

// status is git status in porcelain v2 with its branch headers: the state,
// and the paths with unmerged entries.
func (m Merger) status(ctx context.Context, dir string) (ports.CheckoutState, []string, error) {
	if fi, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) || err == nil && !fi.IsDir() {
		return ports.CheckoutState{}, nil, domain.MergeError(domain.MergeNoCheckout, "no checkout to merge in: "+dir+" is not on disk")
	}
	out, err := m.d.gitOutput(ctx, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		if timedOut(err) {
			return ports.CheckoutState{}, nil, domain.MergeError(domain.MergeFailed, "git did not answer in time reading the checkout at "+dir)
		}
		return ports.CheckoutState{}, nil, domain.MergeError(domain.MergeNoCheckout, "no checkout to merge in: "+dir+" is not a git checkout")
	}
	st, unmerged := parseStatus(out)
	return st, unmerged, nil
}

// parseStatus reads `git status --porcelain=v2 --branch`.
func parseStatus(out string) (ports.CheckoutState, []string) {
	var st ports.CheckoutState
	var unmerged []string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			if oid := strings.TrimPrefix(line, "# branch.oid "); oid != "(initial)" {
				st.Head = oid
			}
		case strings.HasPrefix(line, "# branch.head "):
			if b := strings.TrimPrefix(line, "# branch.head "); b != "(detached)" {
				st.Branch = b
			}
		case strings.HasPrefix(line, "1 "):
			st.Tracked = append(st.Tracked, field(line, 9))
		case strings.HasPrefix(line, "2 "):
			p, _, _ := strings.Cut(field(line, 10), "\t")
			st.Tracked = append(st.Tracked, p)
		case strings.HasPrefix(line, "u "):
			p := field(line, 11)
			st.Tracked = append(st.Tracked, p)
			unmerged = append(unmerged, p)
		case strings.HasPrefix(line, "? "):
			st.Untracked = append(st.Untracked, strings.TrimPrefix(line, "? "))
		}
	}
	return st, unmerged
}

// field is the path at the end of a status entry that has n space-separated
// fields, the last of which may itself hold spaces.
func field(line string, n int) string {
	parts := strings.SplitN(line, " ", n)
	return parts[len(parts)-1]
}

// Merge merges branch into the checked-out branch with --no-ff.
func (m Merger) Merge(ctx context.Context, dir, branch string) (string, error) {
	ref := "refs/heads/" + branch
	// Commits the branch has that HEAD lacks: none means it is merged already,
	// and a merge would change nothing. A branch that is gone fails here.
	out, err := m.d.gitOutput(ctx, dir, "rev-list", "--count", "--end-of-options", "HEAD.."+ref)
	if err != nil {
		if timedOut(err) {
			return "", domain.MergeError(domain.MergeFailed, "git did not answer in time comparing "+branch+" with the checkout")
		}
		if strings.Contains(err.Error(), "bad revision") || strings.Contains(err.Error(), "unknown revision") {
			return "", domain.MergeError(domain.MergeBranchNotFound, "the branch "+branch+" no longer exists")
		}
		return "", domain.MergeError(domain.MergeFailed, err.Error())
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(out)); n == 0 {
		return "", domain.MergeError(domain.MergeAlreadyMerged, branch+" is already merged: the checkout has all of its commits")
	}

	mctx, cancel := context.WithTimeout(ctx, m.d.worktree())
	defer cancel()
	cmd := m.d.gitCmd(mctx, dir, "merge", "--no-ff", "--no-edit", "--quiet", "--end-of-options", ref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if runErr == nil {
		head, err := m.d.gitOutput(ctx, dir, "rev-parse", "--verify", "HEAD")
		if err != nil {
			return "", domain.MergeError(domain.MergeFailed, "the merge went through but HEAD could not be read: "+err.Error())
		}
		return strings.TrimSpace(head), nil
	}
	runErr = cut(mctx, "merge", runErr)
	reason := strings.TrimSpace(strings.TrimSpace(stderr.String()) + "\n" + strings.TrimSpace(stdout.String()))
	return "", m.abort(ctx, dir, branch, runErr, reason)
}

// abort undoes a merge that failed and checks the checkout is what it was: the
// failed merge had not moved HEAD, so HEAD, and an index and working tree with
// no changes, are what the merge started from. A conflict is the refusal the
// person acts on; anything else is a failed merge.
func (m Merger) abort(ctx context.Context, dir, branch string, runErr error, reason string) error {
	// The caller's context may be what ended the merge: undo it regardless.
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.d.worktree())
	defer cancel()
	before, unmerged, _ := m.status(actx, dir)
	// A conflict leaves a merge in progress to abort. A merge that failed
	// after its tree was merged (a hook said no) has none, only the merged
	// tree in the index and the working tree; reset --merge, which --abort
	// is, undoes both.
	undo := []string{"reset", "--merge"}
	if len(unmerged) > 0 {
		undo = []string{"merge", "--abort"}
	}
	if _, err := m.d.gitOutput(actx, dir, undo...); err != nil {
		return domain.MergeError(domain.MergeFailed, "the merge of "+branch+" failed ("+firstLine(reason, runErr)+") and could not be undone: "+err.Error()+". The checkout at "+dir+" needs a look")
	}
	after, still, err := m.status(actx, dir)
	if err != nil || before.Head == "" || after.Head != before.Head || after.Branch != before.Branch || !after.Clean() || len(still) > 0 {
		return domain.MergeError(domain.MergeFailed, "the merge of "+branch+" failed and was aborted, but the checkout at "+dir+" is not as it was; it needs a look")
	}
	if len(unmerged) > 0 {
		return domain.MergeError(domain.MergeConflict, fmt.Sprintf("merging %s conflicts in %s; the merge was aborted and the checkout is as it was", branch, list(unmerged)))
	}
	if timedOut(runErr) {
		return domain.MergeError(domain.MergeFailed, "the merge of "+branch+" took too long and was aborted; the checkout is as it was")
	}
	return domain.MergeError(domain.MergeFailed, "the merge of "+branch+" failed and was aborted, the checkout is as it was: "+firstLine(reason, runErr))
}

func firstLine(reason string, err error) string {
	if reason == "" {
		return err.Error()
	}
	first, _, _ := strings.Cut(reason, "\n")
	return first
}

// list names up to five paths, then how many more.
func list(paths []string) string {
	const max = 5
	if len(paths) <= max {
		return strings.Join(paths, ", ")
	}
	return strings.Join(paths[:max], ", ") + fmt.Sprintf(" and %d more", len(paths)-max)
}
