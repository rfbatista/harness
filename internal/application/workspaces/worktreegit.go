package workspaces

import (
	"context"
	"errors"
	"log/slog"
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
	// Tickets checks a task exists for TaskGitStates; nil skips the check.
	Tickets ports.TicketRepository
	Git     ports.GitWorktreeReader
	Files   ports.WorktreeFiles
	// Publisher reads the origin's URL, Fetcher fetches it (shared with the
	// pull request base refresh) and Feed announces states; each may be nil:
	// FetchRemote then answers UNAVAILABLE, and nothing is announced.
	Publisher ports.BranchPublisher
	Fetcher   *OriginFetcher
	Feed      ports.ProjectChangeSink
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
	s := &WorktreeGit{d: d, slots: map[string]*stateSlot{}}
	if d.Fetcher != nil {
		d.Fetcher.OnFetched(s.afterFetch)
	}
	return s
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
	repo, err := s.repository(ctx, ws.RepositoryID)
	if err != nil {
		return nil, err
	}
	st, err := s.stateOf(ctx, ws, repo)
	if err != nil {
		return nil, err
	}
	s.nameSession(ctx, st)
	return st, nil
}

func (s *WorktreeGit) repository(ctx context.Context, id string) (*domain.Repository, error) {
	repo := s.d.Repositories.Get(ctx, id)
	if repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	if repo.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	return repo, nil
}

// stateOf is a copy of the workspace's state, the held one when it is fresh.
// It names no session.
func (s *WorktreeGit) stateOf(ctx context.Context, ws *domain.Workspace, repo *domain.Repository) (*domain.GitState, error) {
	sl := s.slot(ws.ID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.state != nil && s.d.Now().Sub(sl.state.ReadAt) < CodeWatchInterval {
		return copyState(sl.state), nil
	}
	st, err := s.read(ctx, ws, repo)
	if err != nil {
		return nil, err
	}
	sl.state = st
	return copyState(st), nil
}

// TaskGitStates reads the state of each session workspace of a task, all at
// once: a workspace costs at most one status pass per watch interval, and the
// reads run side by side, bound by ctx.
func (s *WorktreeGit) TaskGitStates(ctx context.Context, taskID string) ([]domain.GitState, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, invalid("task_id is required")
	}
	if s.d.Sessions == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "sessions are not available"}
	}
	if s.d.Tickets != nil && s.d.Tickets.Get(ctx, taskID) == nil {
		return nil, &domain.StructuredError{Code: "TASK_NOT_FOUND", Message: "task not found"}
	}
	found, err := s.d.Sessions.Query(ctx, ports.SessionFilter{TicketID: taskID})
	if err != nil {
		return nil, err
	}
	var rows []*domain.Session
	var ids []string
	for _, sess := range found {
		if sess.WorkspaceID != "" {
			rows = append(rows, sess)
			ids = append(ids, sess.WorkspaceID)
		}
	}
	// The rows are read at once, not one query each.
	byID := map[string]*domain.Workspace{}
	for _, ws := range s.d.Workspaces.ListByIDs(ctx, ids) {
		byID[ws.ID] = ws
	}
	repos := map[string]*domain.Repository{}
	repoErr := map[string]error{}
	for _, ws := range byID {
		if _, seen := repos[ws.RepositoryID]; !seen && repoErr[ws.RepositoryID] == nil {
			repos[ws.RepositoryID], repoErr[ws.RepositoryID] = s.repository(ctx, ws.RepositoryID)
		}
	}
	states := make([]*domain.GitState, len(rows))
	failures := make([]error, len(rows))
	var wg sync.WaitGroup
	for i, sess := range rows {
		ws := byID[sess.WorkspaceID]
		if ws == nil { // the workspace row was deleted
			failures[i] = &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
			continue
		}
		if err := repoErr[ws.RepositoryID]; err != nil {
			failures[i] = err
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := s.stateOf(ctx, ws, repos[ws.RepositoryID])
			if err == nil {
				st.SessionID = sess.ID
			}
			states[i], failures[i] = st, err
		}()
	}
	wg.Wait()
	out := make([]domain.GitState, 0, len(rows))
	for i := range rows {
		switch {
		case errs.Code(failures[i]) == "WORKSPACE_NOT_FOUND": // the workspace row was deleted
		case failures[i] != nil:
			// One worktree git cannot read does not take the task's other
			// states with it; the client has no state for that row.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			slog.Warn("git state not read", "workspace", rows[i].WorkspaceID, "err", failures[i])
		default:
			out = append(out, *states[i])
		}
	}
	return out, nil
}

// FetchRemote fetches the repository's origin, and announces the state of each
// of its workspaces (the fetch's observers do, so a base refresh does too).
func (s *WorktreeGit) FetchRemote(ctx context.Context, repositoryID string) (time.Time, error) {
	if strings.TrimSpace(repositoryID) == "" {
		return time.Time{}, invalid("repository_id is required")
	}
	repo, err := s.repository(ctx, repositoryID)
	if err != nil {
		return time.Time{}, err
	}
	if s.d.Fetcher == nil || s.d.Publisher == nil {
		return time.Time{}, &domain.StructuredError{Code: "UNAVAILABLE", Message: "fetching is not available"}
	}
	origin, err := s.d.Publisher.OriginURL(ctx, repo.RootDir)
	if err != nil {
		return time.Time{}, err
	}
	if origin == "" {
		return time.Time{}, &domain.StructuredError{Code: domain.CodeNoRemote, Message: "the repository has no origin remote"}
	}
	err = s.d.Fetcher.Fetch(ctx, repo.ID, repo.RootDir, origin)
	var failed *OriginFetchError
	if errors.As(err, &failed) {
		return time.Time{}, &domain.StructuredError{Code: domain.CodeFetchFailed, Message: "fetching origin failed: " + failed.Error()}
	} else if err != nil {
		return time.Time{}, err
	}
	if at := s.d.Git.FetchedAt(repo.RootDir); !at.IsZero() {
		return at.UTC(), nil
	}
	return s.d.Now().UTC(), nil
}

// announceParallelism bounds the status passes an announcement runs at once.
const announceParallelism = 8

// afterFetch re-reads and announces the state of every workspace of the
// repository whose origin was just fetched: its counts are new.
func (s *WorktreeGit) afterFetch(ctx context.Context, repositoryID string) {
	if s.d.Feed == nil {
		return
	}
	repo, err := s.repository(ctx, repositoryID)
	if err != nil {
		return
	}
	all := s.d.Workspaces.ListByRepository(repositoryID)
	ids := make([]string, len(all))
	for i, ws := range all {
		ids[i] = ws.ID
	}
	s.forget(ids...)
	sem := make(chan struct{}, announceParallelism)
	var wg sync.WaitGroup
	for _, ws := range all {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.announce(ctx, ws, repo)
		}()
	}
	wg.Wait()
}

// announce reads a workspace's state and puts it on its project's feed. A
// workspace git cannot read is left out.
func (s *WorktreeGit) announce(ctx context.Context, ws *domain.Workspace, repo *domain.Repository) {
	st, err := s.stateOf(ctx, ws, repo)
	if err != nil {
		slog.Warn("git state not announced", "workspace", ws.ID, "err", err)
		return
	}
	s.nameSession(ctx, st)
	s.d.Feed.AnnounceChange(ports.ProjectChange{GitState: &ports.GitStateChange{GitState: st, ProjectID: repo.ProjectID}})
}

// nameSession sets the session that runs in the state's workspace.
func (s *WorktreeGit) nameSession(ctx context.Context, st *domain.GitState) {
	if s.d.Sessions == nil {
		return
	}
	if found, err := s.d.Sessions.Query(ctx, ports.SessionFilter{WorkspaceID: st.WorkspaceID}); err == nil && len(found) > 0 {
		st.SessionID = found[0].ID
	}
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
func (s *WorktreeGit) read(ctx context.Context, ws *domain.Workspace, repo *domain.Repository) (*domain.GitState, error) {
	st := &domain.GitState{WorkspaceID: ws.ID, RepositoryID: repo.ID, Branch: ws.Branch}
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
