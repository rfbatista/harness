package workspaces

import (
	"bytes"
	"cmp"
	"context"
	"path"
	"slices"
	"strings"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.CodeBrowser = (*Code)(nil)

// CodeDeps is what the code views need. Architecture (the project's zones
// and bounded contexts) may be nil: then nothing is zoned.
type CodeDeps struct {
	Workspaces   ports.WorkspaceRepository
	Repositories ports.RepositoryRepository
	Git          ports.GitCode
	Files        ports.WorktreeFiles
	Architecture ports.ArchitectureMap
}

// Code reads code for a person, read-only: a repository at a ref or a
// session's worktree on disk, the diffs of a session, a commit or a range,
// and how the files divide into zones.
type Code struct{ d CodeDeps }

// NewCode returns the use case.
func NewCode(d CodeDeps) *Code { return &Code{d: d} }

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

func pathNotFound(p string) error {
	return &domain.StructuredError{Code: "PATH_NOT_FOUND", Message: p + " is not in this source"}
}

func worktreeMissing(ws *domain.Workspace) error {
	return &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "the worktree " + ws.Path + " is no longer on disk"}
}

// source is a resolved CodeSourceQuery.
type source struct {
	src   domain.CodeSource
	repo  *domain.Repository
	ws    *domain.Workspace // nil for a ref
	zones []*domain.Zone
	match *domain.ZoneMatcher
}

func (s *source) workspace() bool { return s.ws != nil }

// zonesOf are the zones a path belongs to; a directory is matched both as
// itself and as "dir/", so a pattern such as ^internal/ holds internal.
func (s *source) zonesOf(p string, dir bool) []string {
	ids := s.match.Match(p)
	if dir {
		for _, id := range s.match.Match(p + "/") {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) > 1 {
			order := map[string]int{}
			for i, z := range s.zones {
				order[z.ID] = i
			}
			slices.SortFunc(ids, func(a, b string) int { return order[a] - order[b] })
		}
	}
	return ids
}

func (c *Code) repository(id string) (*domain.Repository, error) {
	repo := c.d.Repositories.Get(id)
	if repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	if repo.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	return repo, nil
}

func (c *Code) withZones(s *source) *source {
	if c.d.Architecture != nil {
		s.zones = c.d.Architecture.ListZones(s.repo.ProjectID)
	}
	s.match = domain.NewZoneMatcher(s.zones)
	return s
}

// resolve checks and resolves a source. onDisk refuses a workspace whose
// worktree is gone.
func (c *Code) resolve(ctx context.Context, q ports.CodeSourceQuery, onDisk bool) (*source, error) {
	switch {
	case q.RepositoryID != "" && q.WorkspaceID != "":
		return nil, invalid("name either repository_id or workspace_id, not both")
	case q.RepositoryID == "" && q.WorkspaceID == "":
		return nil, invalid("repository_id or workspace_id is required")
	case q.WorkspaceID != "" && q.Ref != "":
		return nil, invalid("a workspace is read as it is on disk; read its branch with repository_id and ref")
	case q.Ref != "" && !domain.ValidRefName(q.Ref):
		return nil, invalid("ref " + q.Ref + " is not a branch, tag or commit name")
	}
	if q.WorkspaceID != "" {
		return c.resolveWorkspace(ctx, q.WorkspaceID, onDisk)
	}
	repo, err := c.repository(q.RepositoryID)
	if err != nil {
		return nil, err
	}
	ref := q.Ref
	if ref == "" {
		if ref, err = c.d.Git.CurrentBranch(ctx, repo.RootDir); err != nil {
			return nil, err
		}
		if ref == "" {
			ref = "HEAD"
		}
	}
	commit, err := c.d.Git.ResolveCommit(ctx, repo.RootDir, ref)
	if err != nil {
		return nil, err
	}
	s := &source{repo: repo, src: domain.CodeSource{Kind: domain.CodeSourceRef, RepositoryID: repo.ID, Ref: ref, Commit: commit}}
	return c.withZones(s), nil
}

func (c *Code) resolveWorkspace(ctx context.Context, id string, onDisk bool) (*source, error) {
	ws := c.d.Workspaces.Get(id)
	if ws == nil {
		return nil, &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	repo, err := c.repository(ws.RepositoryID)
	if err != nil {
		return nil, err
	}
	gone := !c.d.Files.Exists(ws.Path)
	if gone && onDisk {
		return nil, worktreeMissing(ws)
	}
	wsID := ws.ID
	s := &source{repo: repo, ws: ws, src: domain.CodeSource{
		Kind: domain.CodeSourceWorkspace, RepositoryID: repo.ID, WorkspaceID: &wsID, Ref: ws.Branch, Gone: gone,
	}}
	// HEAD of the worktree; once it is gone, the branch tip in the main
	// checkout. A branch with no commit yet resolves to nothing.
	dir, ref := ws.Path, "HEAD"
	if gone {
		dir, ref = repo.RootDir, ws.Branch
	}
	commit, err := c.d.Git.ResolveCommit(ctx, dir, ref)
	if err != nil && errs.Code(err) != "REF_NOT_FOUND" {
		return nil, err
	}
	s.src.Commit = commit
	return c.withZones(s), nil
}

// cleanPath checks a client path.
func cleanPath(p string) (string, error) {
	clean, ok := domain.CleanCodePath(p)
	if !ok {
		return "", invalid("path " + p + " is not a repository-relative path")
	}
	return clean, nil
}

// CodeTree lists the direct children of the directory dir.
func (c *Code) CodeTree(ctx context.Context, q ports.CodeSourceQuery, dir string) (*domain.CodeTree, error) {
	dir, err := cleanPath(dir)
	if err != nil {
		return nil, err
	}
	s, err := c.resolve(ctx, q, true)
	if err != nil {
		return nil, err
	}
	var entries []domain.TreeEntry
	if s.workspace() {
		entries, err = c.worktreeTree(ctx, s, dir)
	} else {
		entries, err = c.refTree(ctx, s, dir)
	}
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b domain.TreeEntry) int {
		if ad, bd := a.Kind == domain.EntryDir, b.Kind == domain.EntryDir; ad != bd {
			if ad {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return &domain.CodeTree{Source: s.src, Path: dir, Entries: entries}, nil
}

func (c *Code) refTree(ctx context.Context, s *source, dir string) ([]domain.TreeEntry, error) {
	list, err := c.d.Git.TreeAt(ctx, s.repo.RootDir, s.src.Commit, dir, false)
	if err != nil {
		return nil, err
	}
	entries := make([]domain.TreeEntry, 0, len(list))
	for _, g := range list {
		e := domain.TreeEntry{Path: g.Path, Name: path.Base(g.Path), Kind: g.Kind, Zones: s.zonesOf(g.Path, g.Kind == domain.EntryDir)}
		switch g.Kind {
		case domain.EntryFile:
			e.Size = &g.Size
		case domain.EntrySymlink:
			target := g.Target
			e.Target = &target
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// changeSet is what changed in a worktree against its workspace's base:
// each file's state, and every directory holding a change.
type changeSet struct {
	files map[string]string
	dirs  map[string]bool
}

// worktreeChanges compares the worktree with the merge base of its base
// ref, untracked files included. A base that no longer resolves leaves
// every entry without a change.
func (c *Code) worktreeChanges(ctx context.Context, s *source, list []ports.GitEntry) (changeSet, error) {
	set := changeSet{files: map[string]string{}, dirs: map[string]bool{}}
	add := func(p, state string) {
		set.files[p] = state
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			set.dirs[d] = true
		}
		set.dirs[""] = true
	}
	if base, err := c.mergeBase(ctx, s); err == nil {
		changes, err := c.d.Git.Changes(ctx, s.ws.Path, base, "")
		if err != nil {
			return set, err
		}
		for _, ch := range changes {
			state := ch.Status
			if state == domain.ChangeCopied || state == domain.ChangeTypeChanged {
				state = domain.ChangeModified
			}
			add(ch.Path, state)
			if ch.Status == domain.ChangeRenamed {
				add(ch.OldPath, domain.ChangeDeleted)
			}
		}
	} else if errs.Code(err) != "REF_NOT_FOUND" {
		return set, err
	}
	for _, g := range list {
		if g.Untracked {
			add(g.Path, domain.ChangeUntracked)
		}
	}
	return set, nil
}

// baseRef is the ref a workspace was cut from: its recorded base, or the
// branch of the repository's main checkout for one from before base refs
// were recorded.
func (c *Code) baseRef(ctx context.Context, s *source) (string, error) {
	if s.ws.BaseRef != "" {
		return s.ws.BaseRef, nil
	}
	branch, err := c.d.Git.CurrentBranch(ctx, s.repo.RootDir)
	if err != nil {
		return "", err
	}
	if branch == "" {
		return "", &domain.StructuredError{Code: "REF_NOT_FOUND", Message: "the workspace has no base ref and the repository's checkout is detached"}
	}
	return branch, nil
}

// mergeBase is where the workspace's branch forked from its base ref, read
// in the main checkout so it answers after the worktree is gone.
func (c *Code) mergeBase(ctx context.Context, s *source) (string, error) {
	ref, err := c.baseRef(ctx, s)
	if err != nil {
		return "", err
	}
	base, err := c.d.Git.ResolveCommit(ctx, s.repo.RootDir, ref)
	if err != nil {
		return "", err
	}
	if s.src.Commit == "" {
		return base, nil
	}
	return c.d.Git.MergeBase(ctx, s.repo.RootDir, base, s.src.Commit)
}

func (c *Code) worktreeTree(ctx context.Context, s *source, dir string) ([]domain.TreeEntry, error) {
	list, err := c.d.Git.WorktreeEntries(ctx, s.ws.Path)
	if err != nil {
		return nil, err
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	type child struct {
		entry ports.GitEntry
		dir   bool
	}
	children := map[string]child{}
	for _, g := range list {
		rest, ok := strings.CutPrefix(g.Path, prefix)
		if !ok {
			continue
		}
		if name, _, isDir := strings.Cut(rest, "/"); isDir {
			children[name] = child{dir: true}
		} else {
			children[name] = child{entry: g}
		}
	}
	// The directory itself, read without following a link on the way. A
	// directory gone from disk is still listed while git knows files in it.
	if st, err := c.d.Files.Stat(s.ws.Path, dir); err != nil {
		if errs.Code(err) != "PATH_NOT_FOUND" || len(children) == 0 {
			return nil, err
		}
	} else if st.Kind != domain.EntryDir {
		if st.Kind == domain.EntrySymlink && st.Escapes {
			return nil, invalid(dir + " is a symlink out of the worktree")
		}
		return nil, pathNotFound(dir)
	}
	changes, err := c.worktreeChanges(ctx, s, list)
	if err != nil {
		return nil, err
	}
	entries := make([]domain.TreeEntry, 0, len(children))
	for name, ch := range children {
		p := prefix + name
		e := domain.TreeEntry{Path: p, Name: name, Kind: domain.EntryDir, Zones: s.zonesOf(p, ch.dir)}
		if ch.dir {
			if changes.dirs[p] {
				e.Change = strp(domain.ChangeModified)
			}
			entries = append(entries, e)
			continue
		}
		state, changed := changes.files[p]
		if changed {
			e.Change = strp(state)
		}
		e.Kind = ch.entry.Kind
		if ch.entry.Deleted {
			if !changed {
				continue // neither in the base nor on disk
			}
			if e.Kind != domain.EntrySymlink {
				e.Kind = domain.EntryFile
			}
			entries = append(entries, e)
			continue
		}
		if e.Kind != domain.EntrySubmodule {
			st, err := c.d.Files.Stat(s.ws.Path, p)
			if err != nil {
				return nil, err
			}
			switch st.Kind {
			case domain.EntrySymlink:
				e.Kind, e.Target = domain.EntrySymlink, strp(st.Target)
			case domain.EntryDir:
				e.Kind = domain.EntryDir // a directory where git keeps a file
			default:
				e.Kind = domain.EntryFile
				size := st.Size
				e.Size = &size
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func strp(s string) *string { return &s }

// allPaths is every file path of the source in byte order: what git tracks
// at the ref, or a worktree's tracked files on disk and untracked ones.
func (c *Code) allPaths(ctx context.Context, s *source) ([]string, error) {
	var paths []string
	if s.workspace() {
		list, err := c.d.Git.WorktreeEntries(ctx, s.ws.Path)
		if err != nil {
			return nil, err
		}
		for _, g := range list {
			if !g.Deleted {
				paths = append(paths, g.Path)
			}
		}
	} else {
		list, err := c.d.Git.TreeAt(ctx, s.repo.RootDir, s.src.Commit, "", true)
		if err != nil {
			return nil, err
		}
		for _, g := range list {
			paths = append(paths, g.Path)
		}
	}
	slices.Sort(paths)
	return paths, nil
}

// CodePaths lists every file path, at most domain.MaxCodePaths.
func (c *Code) CodePaths(ctx context.Context, q ports.CodeSourceQuery) (*domain.CodePaths, error) {
	s, err := c.resolve(ctx, q, true)
	if err != nil {
		return nil, err
	}
	paths, err := c.allPaths(ctx, s)
	if err != nil {
		return nil, err
	}
	out := &domain.CodePaths{Source: s.src, Paths: paths}
	if len(paths) > domain.MaxCodePaths {
		out.Paths, out.Truncated = paths[:domain.MaxCodePaths], true
	}
	if out.Paths == nil {
		out.Paths = []string{}
	}
	return out, nil
}

// CodeFile reads one file, its content capped at domain.MaxCodeFileBytes.
func (c *Code) CodeFile(ctx context.Context, q ports.CodeSourceQuery, p string) (*domain.CodeFile, error) {
	p, err := cleanPath(p)
	if err != nil {
		return nil, err
	}
	s, err := c.resolve(ctx, q, true)
	if err != nil {
		return nil, err
	}
	if p == "" {
		return nil, pathNotFound("the root")
	}
	var data []byte
	var size int64
	if s.workspace() {
		data, size, err = c.d.Files.Read(s.ws.Path, p, domain.MaxCodeFileBytes+1)
		if err != nil {
			return nil, err
		}
	} else {
		e, err := c.d.Git.PathKind(ctx, s.repo.RootDir, s.src.Commit, p)
		if err != nil {
			return nil, err
		}
		switch {
		case e.Kind == domain.EntrySymlink && domain.SymlinkEscapes(p, e.Target):
			return nil, invalid(p + " is a symlink out of the repository")
		case e.Kind != domain.EntryFile:
			return nil, pathNotFound(p)
		}
		if data, err = c.d.Git.Blob(ctx, s.repo.RootDir, e.Object, domain.MaxCodeFileBytes+1); err != nil {
			return nil, err
		}
		size = e.Size
	}
	f := &domain.CodeFile{Source: s.src, Path: p, Size: size, Language: domain.LanguageOf(p), Zones: s.zonesOf(p, false)}
	if len(data) > domain.MaxCodeFileBytes {
		f.Truncated = true
		data = data[:domain.MaxCodeFileBytes]
		if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
			data = data[:i+1]
		}
	}
	if domain.IsBinary(data) {
		f.Binary = true
		return f, nil
	}
	content := string(data)
	f.Content, f.LineCount = &content, domain.CountLines(content)
	return f, nil
}

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
		list, err := c.d.Git.WorktreeEntries(ctx, plan.dir)
		if err != nil {
			return nil, err
		}
		for _, g := range list {
			if !g.Untracked {
				continue
			}
			ch := ports.GitChange{Status: domain.ChangeUntracked, Path: g.Path, Added: -1, Deleted: 0}
			if only == "" || only == g.Path {
				lines, binary, err := c.d.Files.Lines(plan.dir, g.Path)
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

// CodeMap counts the source's files per bounded context and zone.
func (c *Code) CodeMap(ctx context.Context, q ports.CodeSourceQuery) (*domain.CodeMap, error) {
	s, err := c.resolve(ctx, q, true)
	if err != nil {
		return nil, err
	}
	paths, err := c.allPaths(ctx, s)
	if err != nil {
		return nil, err
	}
	var contexts []*domain.BoundedContext
	if c.d.Architecture != nil {
		contexts = c.d.Architecture.ListBoundedContexts(s.repo.ProjectID)
	}
	contextOf := map[string]int{} // zone id → context index, -1 for none
	known := map[string]int{}
	for i, bc := range contexts {
		known[bc.ID] = i
	}
	for _, z := range s.zones {
		contextOf[z.ID] = -1 // in no context, or one that no longer exists
		if ci, ok := known[z.BoundedContextID]; ok && z.BoundedContextID != "" {
			contextOf[z.ID] = ci
		}
	}
	zoneFiles := make([]int, len(s.zones))
	contextFiles := make([]int, len(contexts))
	out := &domain.CodeMap{Source: s.src, FileCount: len(paths), Contexts: []domain.ContextCount{}, UnassignedZones: []domain.ZoneCount{}}
	for _, p := range paths {
		inContext := map[int]bool{}
		zoned := false
		for zi, z := range s.zones {
			if s.match.Contains(zi, p) {
				zoned = true
				zoneFiles[zi]++
				if ci := contextOf[z.ID]; ci >= 0 {
					inContext[ci] = true
				}
			}
		}
		for ci := range inContext {
			contextFiles[ci]++
		}
		if !zoned {
			out.UnzonedFileCount++
		}
	}
	byName := func(aName, aID, bName, bID string) int {
		return cmp.Or(strings.Compare(aName, bName), strings.Compare(aID, bID))
	}
	counts := make([]domain.ContextCount, len(contexts))
	for ci, bc := range contexts {
		counts[ci] = domain.ContextCount{ID: bc.ID, Name: bc.Name, Purpose: bc.Purpose, FileCount: contextFiles[ci], Zones: []domain.ZoneCount{}}
	}
	for zi, z := range s.zones {
		zc := domain.ZoneCount{ID: z.ID, Name: z.Name, Pattern: z.Pattern, FileCount: zoneFiles[zi]}
		if ci := contextOf[z.ID]; ci >= 0 {
			counts[ci].Zones = append(counts[ci].Zones, zc)
		} else {
			out.UnassignedZones = append(out.UnassignedZones, zc)
		}
	}
	sortZones := func(z []domain.ZoneCount) {
		slices.SortFunc(z, func(a, b domain.ZoneCount) int { return byName(a.Name, a.ID, b.Name, b.ID) })
	}
	for i := range counts {
		sortZones(counts[i].Zones)
	}
	sortZones(out.UnassignedZones)
	slices.SortFunc(counts, func(a, b domain.ContextCount) int { return byName(a.Name, a.ID, b.Name, b.ID) })
	out.Contexts = append(out.Contexts, counts...)
	return out, nil
}
