package workspaces

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.WorktreeGit = (*WorktreeGit)(nil)

// WorktreeGitDeps is what the git state of a worktree is read from. Sessions
// (which session runs in a workspace) may be nil: the state then has no
// session_id.
type WorktreeGitDeps struct {
	Workspaces   ports.WorkspaceRepository
	Repositories ports.RepositoryRepository
	Sessions     ports.SessionRepository
	Git          ports.GitWorktreeReader
	Files        ports.WorktreeFiles
	// Now is the clock the state's age is measured with; nil is time.Now.
	Now func() time.Time
}

// WorktreeGit reads where a session worktree's branch stands against its base
// and its upstream, and what is uncommitted in it. It reads local git only.
// A workspace is read again once its held state is older than
// CodeWatchInterval.
type WorktreeGit struct {
	d WorktreeGitDeps

	mu    sync.Mutex
	slots map[string]*stateSlot // by workspace
}

// stateSlot is the state held for one workspace; its mutex lets one read run
// at a time, and the rest take its answer.
type stateSlot struct {
	mu    sync.Mutex
	state *domain.GitState
}

// NewWorktreeGit returns the use case.
func NewWorktreeGit(d WorktreeGitDeps) *WorktreeGit {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &WorktreeGit{d: d, slots: map[string]*stateSlot{}}
}

func (s *WorktreeGit) slot(workspaceID string) *stateSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.slots[workspaceID]
	if sl == nil {
		sl = &stateSlot{}
		s.slots[workspaceID] = sl
	}
	return sl
}

// GitState reads one workspace's state, from the one held when it is fresh.
func (s *WorktreeGit) GitState(ctx context.Context, workspaceID string) (*domain.GitState, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, invalid("workspace_id is required")
	}
	ws := s.d.Workspaces.Get(ctx, workspaceID)
	if ws == nil {
		return nil, &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	sl := s.slot(ws.ID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.state != nil && s.d.Now().Sub(sl.state.ReadAt) < CodeWatchInterval {
		return copyState(sl.state), nil
	}
	st, err := s.read(ctx, ws)
	if err != nil {
		return nil, err
	}
	sl.state = st
	return copyState(st), nil
}

// forget drops the state held for workspaces, so the next read reads git.
func (s *WorktreeGit) forget(workspaceIDs ...string) {
	for _, id := range workspaceIDs {
		sl := s.slot(id)
		sl.mu.Lock()
		sl.state = nil
		sl.mu.Unlock()
	}
}

func copyState(st *domain.GitState) *domain.GitState {
	cp := *st
	return &cp
}

// read computes a workspace's state from git.
func (s *WorktreeGit) read(ctx context.Context, ws *domain.Workspace) (*domain.GitState, error) {
	repo := s.d.Repositories.Get(ctx, ws.RepositoryID)
	if repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	if repo.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	st := &domain.GitState{WorkspaceID: ws.ID, RepositoryID: repo.ID, Branch: ws.Branch}
	if s.d.Sessions != nil {
		if found, err := s.d.Sessions.Query(ctx, ports.SessionFilter{WorkspaceID: ws.ID}); err == nil && len(found) > 0 {
			st.SessionID = found[0].ID
		}
	}
	fetched := s.d.Git.FetchedAt(repo.RootDir)
	if !fetched.IsZero() {
		st.FetchedAt = &fetched
	}

	var up upstreamCounts
	st.Gone = !s.d.Files.Exists(ws.Path)
	if !st.Gone {
		wt, err := s.d.Git.WorktreeStatus(ctx, ws.Path)
		switch {
		case errs.Code(err) == "WORKSPACE_MISSING":
			st.Gone = true // removed since the check
		case err != nil:
			return nil, err
		default:
			st.Branch, st.Head = wt.Branch, wt.Head
			st.Uncommitted = domain.Uncommitted{Tracked: wt.Tracked, Untracked: wt.Untracked, Staged: wt.Staged}
			up = upstreamCounts{ref: wt.Upstream, gone: wt.UpstreamGone, ahead: wt.Ahead, behind: wt.Behind}
			if i := strings.Index(wt.Upstream, "/"); i > 0 {
				up.remote = wt.Upstream[:i]
			}
		}
	}
	if st.Gone {
		bu, err := s.d.Git.BranchUpstream(ctx, repo.RootDir, ws.Branch)
		if err != nil {
			return nil, err
		}
		up = upstreamCounts{ref: bu.Ref, remote: bu.Remote, gone: bu.Gone, ahead: bu.Ahead, behind: bu.Behind}
		if head, err := s.d.Git.ResolveCommit(ctx, repo.RootDir, "refs/heads/"+ws.Branch); err == nil {
			st.Head = head
		}
	}
	st.Upstream = up.state(st.FetchedAt != nil)
	st.Base = s.baseOf(ctx, ws, repo.RootDir, st.Branch)
	st.ReadAt = s.d.Now().UTC()
	return st, nil
}

// baseOf compares the branch with the ref the workspace was cut from, or with
// the branch checked out in the repository for one from before base refs were
// recorded. A base that does not resolve has no counts.
func (s *WorktreeGit) baseOf(ctx context.Context, ws *domain.Workspace, root, branch string) domain.BaseState {
	ref := ws.BaseRef
	if ref == "" {
		if current, err := s.d.Git.CurrentBranch(ctx, root); err == nil {
			ref = current
		}
	}
	if ref == "" || branch == "" {
		return domain.BaseState{}
	}
	ahead, behind, err := s.d.Git.Divergence(ctx, root, ref, branch)
	if err != nil {
		return domain.BaseState{}
	}
	return domain.BaseState{Ref: ref, Ahead: ahead, Behind: behind}
}

// upstreamCounts is what git says of a branch's upstream.
type upstreamCounts struct {
	ref, remote   string
	gone          bool
	ahead, behind int
}

// state names the upstream's state; a never-fetched repository's counts mean
// nothing, so they are not given.
func (u upstreamCounts) state(fetched bool) domain.UpstreamState {
	if u.ref == "" {
		return domain.UpstreamState{State: domain.UpstreamNone}
	}
	ref := u.ref
	out := domain.UpstreamState{Ref: &ref}
	if u.remote != "" {
		remote := u.remote
		out.Remote = &remote
	}
	switch {
	case u.gone:
		out.State = domain.UpstreamGone
		return out
	case !fetched:
		out.State = domain.UpstreamUnknown
		return out
	}
	ahead, behind := u.ahead, u.behind
	out.Ahead, out.Behind = &ahead, &behind
	switch {
	case ahead > 0 && behind > 0:
		out.State = domain.UpstreamDiverged
	case ahead > 0:
		out.State = domain.UpstreamAhead
	case behind > 0:
		out.State = domain.UpstreamBehind
	default:
		out.State = domain.UpstreamInSync
	}
	return out
}
