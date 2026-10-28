package gitcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitWorktreeReader = (*Code)(nil)

// WorktreeStatus reads a worktree in one status pass: its HEAD and branch, the
// upstream with the counts against it (porcelain v2's branch header), and the
// uncommitted files.
func (c Code) WorktreeStatus(ctx context.Context, dir string) (*ports.GitWorktreeStatus, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "the worktree " + dir + " is no longer on disk"}
	}
	out, err := c.d.codeOutput(ctx, dir, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	st, _ := parseStatusV2(out)
	return st, nil
}

// parseStatusV2 reads `git status --porcelain=v2 --branch -z`: "# branch.*"
// header records, then one record per changed path ("2" records, renames and
// copies, are followed by the original path). It answers the counts and every
// changed path.
func parseStatusV2(out string) (*ports.GitWorktreeStatus, []string) {
	st := &ports.GitWorktreeStatus{}
	var paths []string
	hasAB := false
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		switch {
		case strings.HasPrefix(rec, "# branch.oid "):
			if oid := strings.TrimPrefix(rec, "# branch.oid "); oid != "(initial)" {
				st.Head = oid
			}
		case strings.HasPrefix(rec, "# branch.head "):
			if name := strings.TrimPrefix(rec, "# branch.head "); name != "(detached)" {
				st.Branch = name
			}
		case strings.HasPrefix(rec, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(rec, "# branch.upstream ")
		case strings.HasPrefix(rec, "# branch.ab "):
			hasAB = true
			if f := strings.Fields(strings.TrimPrefix(rec, "# branch.ab ")); len(f) == 2 {
				st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[0], "+"))
				st.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[1], "-"))
			}
		case strings.HasPrefix(rec, "1 "), strings.HasPrefix(rec, "2 "), strings.HasPrefix(rec, "u "):
			st.Tracked++
			staged := rec[0] != 'u' && len(rec) > 3 && rec[2] != '.'
			if staged {
				st.Staged++
			}
			// "1 XY sub mH mI mW hH hI path"; "2" adds the rename score,
			// "u" three more modes and hashes; the path has the rest.
			n := map[byte]int{'1': 9, '2': 10, 'u': 11}[rec[0]]
			if f := strings.SplitN(rec, " ", n); len(f) == n {
				paths = append(paths, f[n-1])
				if staged {
					st.StagedPaths = append(st.StagedPaths, f[n-1])
				}
			}
			if rec[0] == '2' {
				i++ // the original path follows
			}
		case strings.HasPrefix(rec, "? "):
			st.Untracked++
			paths = append(paths, rec[2:])
		}
	}
	// An upstream that is configured but has no counts is a remote-tracking
	// ref that is gone.
	st.UpstreamGone = st.Upstream != "" && !hasAB
	return st, paths
}

// BranchUpstream reads a local branch's upstream in the main checkout.
func (c Code) BranchUpstream(ctx context.Context, repoRoot, branch string) (ports.GitBranchUpstream, error) {
	out, err := c.d.codeOutput(ctx, repoRoot, "for-each-ref", "--format=%(upstream:short)|%(upstream:track,nobracket)|%(upstream:remotename)", "refs/heads/"+branch)
	if err != nil {
		return ports.GitBranchUpstream{}, err
	}
	f := strings.SplitN(strings.TrimSpace(out), "|", 3)
	if len(f) != 3 || f[0] == "" {
		return ports.GitBranchUpstream{}, nil
	}
	up := ports.GitBranchUpstream{Ref: f[0], Remote: f[2]}
	for part := range strings.SplitSeq(f[1], ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "gone":
			up.Gone = true
		case strings.HasPrefix(part, "ahead "):
			up.Ahead, _ = strconv.Atoi(strings.TrimPrefix(part, "ahead "))
		case strings.HasPrefix(part, "behind "):
			up.Behind, _ = strconv.Atoi(strings.TrimPrefix(part, "behind "))
		}
	}
	return up, nil
}

// Divergence counts commits on either side of base...branch.
func (c Code) Divergence(ctx context.Context, dir, base, branch string) (int, int, error) {
	out, err := c.d.codeOutput(ctx, dir, "rev-list", "--left-right", "--count", "--end-of-options", base+"..."+branch, "--")
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

// FetchedAt reads when origin was last fetched from the age of FETCH_HEAD
// (any fetch writes it). A checkout whose .git is no directory has no known
// fetch time.
func (c Code) FetchedAt(repoRoot string) time.Time {
	if fi, err := os.Stat(filepath.Join(repoRoot, ".git", "FETCH_HEAD")); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// SnapshotStatus is Snapshot with the status pass it read: one git run gives
// HEAD, the upstream and counts, and the changed paths the fingerprint is
// made of. The fingerprint covers HEAD and the working tree only, not the
// upstream, so a push or a fetch does not look like a code change.
func (c Code) SnapshotStatus(ctx context.Context, dir string) (string, string, *ports.GitWorktreeStatus, error) {
	out, err := c.d.codeOutput(ctx, dir, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return "", "", nil, err
	}
	st, paths := parseStatusV2(out)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00", st.Head)
	for _, rec := range strings.Split(out, "\x00") {
		if !strings.HasPrefix(rec, "# ") {
			fmt.Fprintf(h, "%s\x00", rec)
		}
	}
	for _, p := range paths {
		if info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			fmt.Fprintf(h, "%s %d %d\x00", p, info.Size(), info.ModTime().UnixNano())
		}
	}
	return st.Head, hex.EncodeToString(h.Sum(nil)), st, nil
}
