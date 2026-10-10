package code

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Seed is what a code page's first paint read, in the contract's wire
// shapes, for the browser to take over without asking again (dto.js toSeed).
type Seed struct {
	ProjectID      string             `json:"project_id"`
	Route          Route              `json:"route"`
	Source         SourceQuery        `json:"source"`
	RepositoryName string             `json:"repository_name"`
	Branch         string             `json:"branch"`
	BaseRef        string             `json:"base_ref"`
	Live           bool               `json:"live"`
	Worktrees      []Worktree         `json:"worktrees,omitempty"`
	Trees          []*domain.CodeTree `json:"trees"`
	File           *domain.CodeFile   `json:"file"`
	Diff           *domain.CodeDiff   `json:"diff"`
	Map            *domain.CodeMap    `json:"map"`
	Error          *SeedError         `json:"error"`
	Refs           []string           `json:"refs,omitempty"`
	HistoryHref    string             `json:"history_href,omitempty"`
	// Commit is the history page's open commit ("" for its uncommitted changes).
	Commit string `json:"commit,omitempty"`
	// Gone: the worktree is no longer on disk; BaseGone: its base branch no
	// longer resolves (contract §10 C7), so Changes shows scope working.
	Gone     bool `json:"gone,omitempty"`
	BaseGone bool `json:"base_gone,omitempty"`
}

// SourceQuery is the source a page reads, in the wire's names.
type SourceQuery struct {
	RepositoryID string `json:"repository_id,omitempty"`
	Ref          string `json:"ref,omitempty"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
}

// Worktree is one of a session's worktrees, for the source bar's select.
type Worktree struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SeedError is what the first paint hit, by its code.
type SeedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Source names what a page reads and how to describe it.
type Source struct {
	Query          ports.CodeSourceQuery
	RepositoryName string
	// Branch is a session's branch; BaseRef the branch it was cut from.
	Branch, BaseRef string
	// Live: the session is running, so the view follows its worktree.
	Live bool
}

func (s Source) query() SourceQuery {
	return SourceQuery{RepositoryID: s.Query.RepositoryID, Ref: s.Query.Ref, WorkspaceID: s.Query.WorkspaceID}
}

func toSeedError(err error) *SeedError {
	if err == nil {
		return nil
	}
	code := errs.Code(err)
	if code == "" {
		code = "UNKNOWN"
	}
	msg := err.Error()
	var se *domain.StructuredError
	if errors.As(err, &se) {
		msg = se.Message
	}
	return &SeedError{Code: code, Message: msg}
}

// Load reads what the route's first paint shows: one diff, the tree levels
// down to the open path and that file, or the map; and the map beside them
// for the zone tags (read concurrently). A failure is painted from the seed,
// not returned.
func Load(ctx context.Context, cb ports.CodeBrowser, rt Route, src Source) Seed {
	seed := Seed{
		ProjectID: rt.ProjectID, Route: rt, Source: src.query(), RepositoryName: src.RepositoryName,
		Branch: src.Branch, BaseRef: src.BaseRef, Live: src.Live,
	}
	session := rt.Kind == "session"

	var wg sync.WaitGroup
	var zoneMap *domain.CodeMap
	if rt.View != ViewMap {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The map only names zones; without it the tags are left out.
			zoneMap, _ = cb.CodeMap(ctx, src.Query)
		}()
	}

	switch {
	case rt.View == ViewMap:
		m, err := cb.CodeMap(ctx, src.Query)
		seed.Map, seed.Error = m, toSeedError(err)
	case session && (rt.View == ViewChanges || rt.View == ""):
		seed.loadDiff(ctx, cb, rt)
		if rt.View == "" && seed.Diff != nil && len(seed.Diff.Files) == 0 && !seed.Gone {
			// Nothing changed yet: the tab opens on Files (UX §2.1).
			seed.Route.View = ViewFiles
			seed.loadFiles(ctx, cb, src.Query, "")
		}
	default:
		if seed.Route.View == "" {
			seed.Route.View = ViewFiles
		}
		seed.loadFiles(ctx, cb, src.Query, rt.Path)
	}
	wg.Wait()
	if seed.Map == nil {
		seed.Map = zoneMap
	}
	if seed.Error != nil && seed.Error.Code == "WORKSPACE_MISSING" {
		seed.Gone = true
	}
	return seed
}

// loadDiff reads the session's diff in its scope; a gone worktree falls back
// to its branch, a gone base to the uncommitted work (UX §5).
func (s *Seed) loadDiff(ctx context.Context, cb ports.CodeBrowser, rt Route) {
	ws := s.Source.WorkspaceID
	scope := rt.Scope
	if scope == "" {
		scope = "all"
	}
	d, err := cb.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: ws, Scope: scope})
	if errs.Code(err) == "WORKSPACE_MISSING" {
		s.Gone = true
		d, err = cb.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: ws, Scope: "branch"})
	}
	if errs.Code(err) == "REF_NOT_FOUND" && scope != "working" {
		s.BaseGone = true
		d, err = cb.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: ws, Scope: "working"})
	}
	if d != nil && d.Source.Gone {
		s.Gone = true
	}
	s.Diff, s.Error = d, toSeedError(err)
}

// loadFiles reads the root and every directory down to path, then path
// itself: a directory's level, or a file. A symlink, a submodule or a
// deleted file is left to the browser's placeholder or diff (C3, C8).
func (s *Seed) loadFiles(ctx context.Context, cb ports.CodeBrowser, q ports.CodeSourceQuery, path string) {
	dirs := []string{""}
	if path != "" {
		parts := strings.Split(path, "/")
		for i := 1; i < len(parts); i++ {
			dirs = append(dirs, strings.Join(parts[:i], "/"))
		}
	}
	var entry *domain.TreeEntry
	for _, dir := range dirs {
		t, err := cb.CodeTree(ctx, q, dir)
		if err != nil {
			s.Error = toSeedError(err)
			if dir != "" && errs.Code(err) == "PATH_NOT_FOUND" {
				s.Error.Message = path
			}
			return
		}
		s.Trees = append(s.Trees, t)
		entry = nil
		for i := range t.Entries {
			if t.Entries[i].Path == path {
				entry = &t.Entries[i]
			}
		}
	}
	if path == "" {
		return
	}
	if entry == nil {
		s.Error = &SeedError{Code: "PATH_NOT_FOUND", Message: path}
		return
	}
	switch {
	case entry.Kind == domain.EntryDir:
		t, err := cb.CodeTree(ctx, q, path)
		if err != nil {
			s.Error = toSeedError(err)
			return
		}
		s.Trees = append(s.Trees, t)
	case entry.Kind == domain.EntryFile && (entry.Change == nil || *entry.Change != domain.ChangeDeleted):
		f, err := cb.CodeFile(ctx, q, path)
		s.File, s.Error = f, toSeedError(err)
	}
}

// LoadCommit reads one commit's diff against its first parent, for the
// history page's open commit.
func LoadCommit(ctx context.Context, cb ports.CodeBrowser, projectID, repositoryID, commit string) Seed {
	// View file opens the repository's code page at the commit.
	rt := Route{Kind: "repository", ProjectID: projectID, RepositoryID: repositoryID, View: ViewChanges, Ref: commit}
	d, err := cb.CodeDiff(ctx, ports.CodeDiffQuery{RepositoryID: repositoryID, Commit: commit})
	return Seed{ProjectID: projectID, Route: rt, Source: SourceQuery{RepositoryID: repositoryID}, Diff: d, Error: toSeedError(err), Commit: commit}
}

// LoadWorking reads a worktree's uncommitted changes (scope working), for
// the session history page's working-copy row.
func LoadWorking(ctx context.Context, cb ports.CodeBrowser, projectID, repositoryID, workspaceID string) Seed {
	// Kind "history": uncommitted files have no code page to open them on.
	rt := Route{Kind: "history", ProjectID: projectID, RepositoryID: repositoryID, View: ViewChanges}
	d, err := cb.CodeDiff(ctx, ports.CodeDiffQuery{WorkspaceID: workspaceID, Scope: "working"})
	return Seed{ProjectID: projectID, Route: rt, Source: SourceQuery{WorkspaceID: workspaceID}, Diff: d, Error: toSeedError(err)}
}
