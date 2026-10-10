package workspaces

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// diffPlan is what a diff query resolved to: where git runs, the two sides
// (head "" the worktree), and whether untracked files count.
type diffPlan struct {
	src       *source
	scope     string
	dir       string
	base      string
	head      string
	untracked bool
}

func (c *Code) planDiff(ctx context.Context, q ports.CodeDiffQuery) (*diffPlan, error) {
	if q.WorkspaceID != "" {
		if q.RepositoryID != "" || q.Commit != "" || q.Base != "" || q.Head != "" {
			return nil, invalid("a workspace diff takes workspace_id and scope only")
		}
		return c.planWorkspaceDiff(ctx, q)
	}
	if q.RepositoryID == "" {
		return nil, invalid("repository_id or workspace_id is required")
	}
	if q.Scope != "" {
		return nil, invalid("scope is for a workspace's diff")
	}
	switch {
	case q.Commit != "" && (q.Base != "" || q.Head != ""):
		return nil, invalid("name either commit or base and head")
	case q.Commit != "":
		if !domain.ValidCommitHash(q.Commit) {
			return nil, invalid("commit " + q.Commit + " is not a commit hash")
		}
	case q.Base == "" || q.Head == "":
		return nil, invalid("a repository diff needs commit, or both base and head")
	case !domain.ValidRefName(q.Base) || !domain.ValidRefName(q.Head):
		return nil, invalid("base and head must be branch, tag or commit names")
	}
	repo, err := c.repository(q.RepositoryID)
	if err != nil {
		return nil, err
	}
	s := c.withZones(&source{repo: repo, src: domain.CodeSource{Kind: domain.CodeSourceRef, RepositoryID: repo.ID}})
	plan := &diffPlan{src: s, dir: repo.RootDir}
	if q.Commit != "" {
		commit, err := c.d.Git.ResolveCommit(ctx, repo.RootDir, q.Commit)
		if errs.Code(err) == "REF_NOT_FOUND" {
			return nil, &domain.StructuredError{Code: "COMMIT_NOT_FOUND", Message: "commit " + q.Commit + " not found in this repository"}
		} else if err != nil {
			return nil, err
		}
		s.src.Ref, s.src.Commit, plan.scope, plan.head = q.Commit, commit, domain.ScopeCommit, commit
		if plan.base, err = c.d.Git.FirstParent(ctx, repo.RootDir, commit); err != nil {
			return nil, err
		}
		if plan.base == "" {
			if plan.base, err = c.d.Git.EmptyTree(ctx, repo.RootDir); err != nil {
				return nil, err
			}
		}
		return plan, nil
	}
	base, err := c.d.Git.ResolveCommit(ctx, repo.RootDir, q.Base)
	if err != nil {
		return nil, err
	}
	head, err := c.d.Git.ResolveCommit(ctx, repo.RootDir, q.Head)
	if err != nil {
		return nil, err
	}
	if plan.base, err = c.d.Git.MergeBase(ctx, repo.RootDir, base, head); err != nil {
		return nil, invalid(q.Base + " and " + q.Head + " have no common ancestor")
	}
	s.src.Ref, s.src.Commit, plan.scope, plan.head = q.Head, head, domain.ScopeRange, head
	return plan, nil
}

func (c *Code) planWorkspaceDiff(ctx context.Context, q ports.CodeDiffQuery) (*diffPlan, error) {
	scope := cmp.Or(q.Scope, domain.ScopeAll)
	if scope != domain.ScopeAll && scope != domain.ScopeBranch && scope != domain.ScopeWorking {
		return nil, invalid("scope " + q.Scope + " is not all, branch or working")
	}
	s, err := c.resolveWorkspace(ctx, q.WorkspaceID, false)
	if err != nil {
		return nil, err
	}
	if s.src.Gone && scope != domain.ScopeBranch {
		return nil, worktreeMissing(s.ws)
	}
	plan := &diffPlan{src: s, scope: scope, dir: s.ws.Path, untracked: true}
	switch scope {
	case domain.ScopeWorking:
		if s.src.Commit == "" {
			return nil, &domain.StructuredError{Code: "REF_NOT_FOUND", Message: "the worktree has no commit to compare with"}
		}
		plan.base = s.src.Commit
	case domain.ScopeAll:
		if plan.base, err = c.mergeBase(ctx, s); err != nil {
			return nil, err
		}
	case domain.ScopeBranch:
		if plan.base, err = c.mergeBase(ctx, s); err != nil {
			return nil, err
		}
		plan.dir, plan.head, plan.untracked = s.repo.RootDir, s.src.Commit, false
	}
	return plan, nil
}

// CodeDiff reads the changed files with hunks within the diff limits.
func (c *Code) CodeDiff(ctx context.Context, q ports.CodeDiffQuery) (*domain.CodeDiff, error) {
	only, err := cleanPath(q.Path)
	if err != nil {
		return nil, err
	}
	plan, err := c.planDiff(ctx, q)
	if err != nil {
		return nil, err
	}
	changes, err := c.d.Git.Changes(ctx, plan.dir, plan.base, plan.head)
	if err != nil {
		return nil, err
	}
	if plan.untracked {
		untracked, err := c.d.Git.UntrackedFiles(ctx, plan.dir)
		if err != nil {
			return nil, err
		}
		for _, p := range untracked {
			ch := ports.GitChange{Status: domain.ChangeUntracked, Path: p, Added: -1, Deleted: 0}
			if only == "" || only == p {
				lines, binary, err := c.d.Files.Lines(plan.dir, p)
				if err != nil {
					return nil, err
				}
				if !binary {
					ch.Added = lines
				} else {
					ch.Deleted = -1
				}
			}
			changes = append(changes, ch)
		}
	}
	if only != "" {
		changes = slices.DeleteFunc(changes, func(ch ports.GitChange) bool { return ch.Path != only && ch.OldPath != only })
		if len(changes) == 0 {
			return nil, &domain.StructuredError{Code: "PATH_NOT_FOUND", Message: only + " did not change"}
		}
	}
	slices.SortFunc(changes, func(a, b ports.GitChange) int { return strings.Compare(a.Path, b.Path) })

	out := &domain.CodeDiff{Source: plan.src.src, Base: plan.base, Scope: plan.scope, Files: make([]domain.FileDiff, 0, len(changes))}
	var tracked, untracked []string
	total := 0
	for _, ch := range changes {
		fd := domain.FileDiff{
			Path: ch.Path, Status: ch.Status, Added: ch.Added, Deleted: ch.Deleted,
			Language: domain.LanguageOf(ch.Path), Zones: plan.src.zonesOf(ch.Path, false),
		}
		if ch.OldPath != "" {
			fd.OldPath = strp(ch.OldPath)
		}
		changed := ch.Added + ch.Deleted
		switch {
		case ch.Added < 0 || ch.Deleted < 0:
			fd.Binary, fd.Added, fd.Deleted, fd.Withheld = true, -1, -1, strp(domain.WithheldBinary)
		case only != "":
			// One file: hunks up to the hard cap, checked while reading.
		case changed > domain.MaxDiffFileLines:
			fd.Withheld = strp(domain.WithheldTooLarge)
		case total+changed > domain.MaxDiffLines:
			fd.Withheld, out.Truncated = strp(domain.WithheldTooLarge), true
		default:
			total += changed
		}
		if !fd.Binary {
			out.Added += fd.Added
			out.Deleted += fd.Deleted
		}
		if fd.Withheld == nil {
			if ch.Status == domain.ChangeUntracked {
				untracked = append(untracked, ch.Path)
			} else {
				tracked = append(tracked, ch.Path)
				if ch.OldPath != "" {
					tracked = append(tracked, ch.OldPath)
				}
			}
		}
		out.Files = append(out.Files, fd)
	}
	maxLines := domain.MaxDiffFileLines
	if only != "" {
		maxLines = domain.MaxDiffLines
	}
	patches := map[string]*domain.FileDiff{}
	if len(tracked) > 0 {
		if patches, err = c.d.Git.Patch(ctx, plan.dir, plan.base, plan.head, tracked, maxLines, domain.MaxDiffFileBytes); err != nil {
			return nil, err
		}
	}
	for i := range out.Files {
		fd := &out.Files[i]
		if fd.Withheld != nil {
			continue
		}
		if fd.Status == domain.ChangeUntracked {
			if err := c.untrackedHunk(plan.dir, fd, maxLines); err != nil {
				return nil, err
			}
			continue
		}
		fd.Hunks = []domain.Hunk{}
		if p := patches[fd.Path]; p != nil {
			switch {
			case p.Withheld != nil && *p.Withheld == domain.WithheldBinary:
				fd.Binary, fd.Withheld = true, p.Withheld
				fd.Hunks = nil
			case p.Withheld != nil:
				fd.Withheld, fd.Hunks = p.Withheld, nil
			case p.Hunks != nil:
				fd.Hunks = p.Hunks
			}
		}
	}
	return out, nil
}

// untrackedHunk builds an untracked file's one all-added hunk from disk.
func (c *Code) untrackedHunk(dir string, fd *domain.FileDiff, maxLines int) error {
	if fd.Added > maxLines {
		fd.Withheld = strp(domain.WithheldTooLarge)
		return nil
	}
	data, size, err := c.d.Files.Read(dir, fd.Path, domain.MaxDiffFileBytes+1)
	if err != nil {
		return err
	}
	if size > domain.MaxDiffFileBytes {
		fd.Withheld = strp(domain.WithheldTooLarge)
		return nil
	}
	fd.Hunks = []domain.Hunk{}
	if len(data) == 0 {
		return nil
	}
	text := string(data)
	noNewline := !strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	h := domain.Hunk{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: len(lines), Lines: make([]domain.HunkLine, len(lines))}
	for i, l := range lines {
		n := i + 1
		h.Lines[i] = domain.HunkLine{Kind: domain.LineAdd, New: &n, Text: l}
	}
	h.Lines[len(lines)-1].NoNewline = noNewline
	fd.Hunks = []domain.Hunk{h}
	return nil
}
