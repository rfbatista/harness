package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Branches names the local branches and the default one in one for-each-ref.
// The default branch is named by origin/HEAD, else main, else master, as the
// merge state names it, and is the local branch of that name.
func (m Merger) Branches(ctx context.Context, dir string) (ports.BranchList, error) {
	out, err := m.d.gitOutput(ctx, dir, "for-each-ref", "--format=%(refname) %(symref)", "refs/heads/", "refs/remotes/origin/HEAD")
	if err != nil {
		return ports.BranchList{}, err
	}
	wts, err := m.d.gitOutput(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return ports.BranchList{}, err
	}
	var res ports.BranchList
	originHead := ""
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if name, ok := strings.CutPrefix(f[0], "refs/heads/"); ok {
			res.Names = append(res.Names, name)
		} else if len(f) == 2 {
			originHead = strings.TrimPrefix(f[1], "refs/remotes/origin/")
		}
	}
	// The first worktree listed is the main checkout; a branch checked out in
	// any other is in use by someone, and cannot be moved from under them.
	for i, block := range strings.Split(strings.TrimSpace(wts), "\n\n") {
		if b, ok := strings.CutPrefix(worktreeBranchLine(block), "refs/heads/"); ok && i > 0 {
			res.InOtherWorktrees = append(res.InOtherWorktrees, b)
		}
	}
	for _, name := range []string{originHead, "main", "master"} {
		if name != "" && slices.Contains(res.Names, name) {
			res.Default = name
			break
		}
	}
	return res, nil
}

// Ahead counts the commits branch has that target lacks.
func (m Merger) Ahead(ctx context.Context, dir, target, branch string) (int, error) {
	out, err := m.d.gitOutput(ctx, dir, "rev-list", "--count", "--end-of-options", "refs/heads/"+target+"..refs/heads/"+branch)
	if err != nil {
		return 0, notAnswered(err, "counting the commits of "+branch+" that "+target+" lacks")
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n, nil
}

// Tip is the commit branch points at.
func (m Merger) Tip(ctx context.Context, dir, branch string) (string, error) {
	out, err := m.d.gitOutput(ctx, dir, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+branch)
	if err != nil {
		return "", notAnswered(err, "reading "+branch)
	}
	return strings.TrimSpace(out), nil
}

// Subject is the subject line of branch's newest commit.
func (m Merger) Subject(ctx context.Context, dir, branch string) (string, error) {
	out, err := m.d.gitOutput(ctx, dir, "log", "-1", "--format=%s", "--end-of-options", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// notAnswered is a failed or slow read as a coded merge failure.
func notAnswered(err error, doing string) error {
	if timedOut(err) {
		return domain.MergeError(domain.MergeFailed, "git did not answer in time "+doing)
	}
	return domain.MergeError(domain.MergeFailed, err.Error())
}

// MergeInto merges branch into target in git's object store and moves target
// to the result, so a target that is not checked out is merged without the
// main checkout's working tree, index or HEAD being read or written. A
// conflict is found by merge-tree before anything is written, and so changes
// nothing. A target that is checked out (checkedOut) then has its files
// brought along by merge --ff-only, which refuses rather than overwrite a change
// of the person's. The merge commit's message is git's own; commit-tree runs
// no hooks.
func (m Merger) MergeInto(ctx context.Context, dir, target, branch string, method domain.MergeMethod, message string, checkedOut bool) (string, error) {
	tref, bref := "refs/heads/"+target, "refs/heads/"+branch
	out, err := m.d.gitOutput(ctx, dir, "for-each-ref", "--format=%(refname) %(objectname)", tref, bref)
	if err != nil {
		return "", notAnswered(err, "reading "+target+" and "+branch)
	}
	tips := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			tips[f[0]] = f[1]
		}
	}
	targetTip, branchTip := tips[tref], tips[bref]
	switch {
	case branchTip == "":
		return "", domain.MergeError(domain.MergeBranchNotFound, "the branch "+branch+" no longer exists")
	case targetTip == "":
		return "", domain.MergeError(domain.MergeTargetNotFound, "the target "+target+" is not a local branch")
	}
	if n, err := m.Ahead(ctx, dir, target, branch); err != nil {
		return "", err
	} else if n == 0 {
		return "", domain.MergeError(domain.MergeAlreadyMerged, branch+" is already merged: "+target+" has all of its commits")
	}

	tree, files, err := m.mergeTree(ctx, dir, tref, bref)
	if err != nil {
		return "", err
	}
	if tree == "" {
		return "", conflict(branch, target, files)
	}

	args := []string{"commit-tree", tree, "-p", targetTip}
	switch method {
	case domain.MergeMethodSquash:
		args = append(args, "-m", message)
	default:
		args = append(args, "-p", branchTip, "-m", "Merge branch '"+branch+"' into "+target)
	}
	commit, err := m.d.gitOutput(ctx, dir, args...)
	if err != nil {
		return "", domain.MergeError(domain.MergeFailed, "the merge of "+branch+" into "+target+" could not be committed: "+err.Error())
	}
	commit = strings.TrimSpace(commit)

	wctx, cancel := context.WithTimeout(ctx, m.d.worktree())
	defer cancel()
	if checkedOut {
		// A fast-forward only: it refuses if HEAD moved since the tip was read,
		// and keeps (or refuses over) the person's local changes.
		_, err = m.d.gitRun(wctx, dir, "merge", "--ff-only", "--quiet", commit)
	} else {
		_, err = m.d.gitRun(wctx, dir, "update-ref", "-m", "merge "+branch+": "+string(method), tref, commit, targetTip)
	}
	if err != nil {
		return "", domain.MergeError(domain.MergeFailed, "the merge of "+branch+" into "+target+" was prepared but "+target+" could not be moved, so nothing changed: "+err.Error())
	}
	return commit, nil
}

// mergeTree merges two refs in the object store. It answers the merged tree,
// or, on a conflict, no tree and the paths in conflict.
func (m Merger) mergeTree(ctx context.Context, dir, tref, bref string) (string, []string, error) {
	rctx, cancel := context.WithTimeout(ctx, m.d.worktree())
	defer cancel()
	cmd := m.d.gitCmd(rctx, dir, "merge-tree", "--write-tree", "--name-only", "--no-messages", tref, bref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if err == nil && lines[0] != "" {
		return lines[0], nil, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		var files []string
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l == "" {
				break
			} else {
				files = append(files, l)
			}
		}
		return "", files, nil
	}
	if err = cut(rctx, "merge-tree", err); timedOut(err) {
		return "", nil, domain.MergeError(domain.MergeFailed, "git did not answer in time merging the branches")
	}
	return "", nil, domain.MergeError(domain.MergeFailed, "merging the branches failed: "+strings.TrimSpace(stderr.String()+" "+fmt.Sprint(err)))
}

// conflict is MERGE_CONFLICT naming the paths, with them as details.
func conflict(branch, target string, files []string) error {
	msg := fmt.Sprintf("merging %s into %s conflicts", branch, target)
	if len(files) > 0 {
		msg += " in " + list(files)
	}
	msg += "; nothing was changed"
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.MergeConflict, Message: msg},
		Details:         map[string]any{"files": files},
	}
}

// worktreeBranchLine is the "branch" value of one block of
// git worktree list --porcelain.
func worktreeBranchLine(block string) string {
	for _, l := range strings.Split(block, "\n") {
		if v, ok := strings.CutPrefix(l, "branch "); ok {
			return v
		}
	}
	return ""
}
