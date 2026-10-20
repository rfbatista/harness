package gitcli

import (
	"context"
	"strconv"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.BranchMergeReader = (*BranchMerges)(nil)

// BranchMerges reads whether branches landed in a repository's default
// branch. It only reads, each git run bounded by the read deadline.
type BranchMerges struct{ d config }

// NewBranchMerges returns the adapter.
func NewBranchMerges(opts ...Option) *BranchMerges { return &BranchMerges{d: newConfig(opts)} }

// BranchMerges costs two git runs for the repository and one per branch.
//
// The default branch is named by origin/HEAD, else main, else master, and is
// the local branch of that name, so a merge not yet pushed counts.
//
// A branch has commits of its own when its tip moved since it was cut. Its
// base by name cannot tell: once the branch is merged the base holds every
// commit of it. The commit the workspace recorded when it was cut can. A
// workspace made before that was recorded falls back to the branch's reflog,
// whose oldest entry is where it was created, and without a reflog (expired,
// or core.logAllRefUpdates off) to the commits ahead of its base, which
// misses a branch already merged.
func (m BranchMerges) BranchMerges(ctx context.Context, root string, branches []ports.BranchBase) (ports.BranchMerges, error) {
	out, err := m.d.gitOutput(ctx, root, "for-each-ref", "--format=%(refname) %(objectname) %(symref)", "refs/heads/", "refs/remotes/origin/HEAD")
	if err != nil {
		return ports.BranchMerges{}, err
	}
	tips, originHead := map[string]string{}, ""
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if name, ok := strings.CutPrefix(f[0], "refs/heads/"); ok {
			tips[name] = f[1]
		} else if len(f) == 3 {
			originHead = strings.TrimPrefix(f[2], "refs/remotes/origin/")
		}
	}
	res := ports.BranchMerges{Branches: map[string]ports.BranchMerge{}}
	for _, name := range []string{originHead, "main", "master"} {
		if _, ok := tips[name]; ok && name != "" {
			res.DefaultBranch = name
			break
		}
	}
	merged := map[string]bool{}
	if res.DefaultBranch != "" {
		out, err := m.d.gitOutput(ctx, root, "for-each-ref", "--format=%(refname)", "--merged=refs/heads/"+res.DefaultBranch, "refs/heads/")
		if err != nil {
			return ports.BranchMerges{}, err
		}
		for _, ref := range strings.Fields(out) {
			merged[strings.TrimPrefix(ref, "refs/heads/")] = true
		}
	}
	for _, b := range branches {
		tip, ok := tips[b.Branch]
		if !ok {
			res.Branches[b.Branch] = ports.BranchMerge{}
			continue
		}
		ahead, err := m.ahead(ctx, root, b, tip, res.DefaultBranch)
		if err != nil {
			return ports.BranchMerges{}, err
		}
		res.Branches[b.Branch] = ports.BranchMerge{Exists: true, Ahead: ahead, Merged: merged[b.Branch]}
	}
	return res, nil
}

// ahead reports whether branch b, at tip, moved since it was cut.
func (m BranchMerges) ahead(ctx context.Context, root string, b ports.BranchBase, tip, defaultBranch string) (bool, error) {
	if b.BaseSHA != "" {
		return tip != b.BaseSHA, nil
	}
	out, err := m.d.gitOutput(ctx, root, "log", "-g", "--format=%H", "refs/heads/"+b.Branch, "--")
	if err != nil {
		return false, err
	}
	if lines := strings.Fields(out); len(lines) > 0 {
		return lines[len(lines)-1] != tip, nil
	}
	base := b.Base
	if base == "" {
		base = defaultBranch
	}
	if base == "" {
		return true, nil
	}
	out, err = m.d.gitOutput(ctx, root, "rev-list", "--count", base+".."+tip, "--")
	if err != nil {
		if ctx.Err() != nil || timedOut(err) {
			return false, err
		}
		// A base that no longer resolves cannot rule the branch out.
		return true, nil
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n > 0, nil
}
