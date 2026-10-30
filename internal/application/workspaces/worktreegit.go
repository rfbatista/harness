package workspaces

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
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

	mu        sync.Mutex
	slots     map[string]*stateSlot  // by workspace
	pushLocks map[string]*sync.Mutex // per workspace: one push at a time
}

// stateSlot is the state held for one workspace; its mutex lets one read run
// at a time, and the rest take its answer.
type stateSlot struct {
	mu    sync.Mutex
	state *domain.GitState
	// src is the status pass the state was composed from, and fetched the
	// repository's fetch time then; a watcher that sees both unchanged knows
	// the state still holds. baseAt is when the base comparison was last read.
	src     *ports.GitWorktreeStatus
	fetched time.Time
	baseAt  time.Time
	// observed: the code watcher keeps the state current, so it is served
	// while the watcher's polls keep confirming it.
	observed bool
	// sessionID is the session running in the workspace, read once for the
	// states a watcher announces.
	sessionID string
}

// NewWorktreeGit returns the use case.
func NewWorktreeGit(d WorktreeGitDeps) *WorktreeGit {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &WorktreeGit{d: d, slots: map[string]*stateSlot{}, pushLocks: map[string]*sync.Mutex{}}
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

// stateOf is a copy of the workspace's state, the held one when it is fresh:
// read within the watch interval, or confirmed by the code watcher within
// three. It names no session.
func (s *WorktreeGit) stateOf(ctx context.Context, ws *domain.Workspace, repo *domain.Repository) (*domain.GitState, error) {
	sl := s.slot(ws.ID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.state != nil {
		window := CodeWatchInterval
		if sl.observed {
			window = 3 * CodeWatchInterval
		}
		if s.d.Now().Sub(sl.state.ReadAt) < window {
			return copyState(sl.state), nil
		}
	}
	st, src, err := s.read(ctx, ws, repo, nil)
	if err != nil {
		return nil, err
	}
	s.hold(sl, st, src, repo)
	return copyState(st), nil
}

// hold keeps a state just read in its slot. The slot is locked.
func (s *WorktreeGit) hold(sl *stateSlot, st *domain.GitState, src *ports.GitWorktreeStatus, repo *domain.Repository) {
	sl.state, sl.src, sl.baseAt, sl.observed = st, src, st.ReadAt, false
	sl.fetched = s.d.Git.FetchedAt(repo.RootDir)
}

// baseRecheck is how long a held base comparison is trusted while the code
// watcher confirms the rest: the base branch may move with the worktree idle.
const baseRecheck = 30 * time.Second

// StateObserver is what the code watcher tells of a running worktree: the
// status pass of its poll, with no git run of its own.
type StateObserver interface {
	// ObserveGitState brings the workspace's held state up to the status the
	// watcher just read, and answers it, and whether it changed. Nothing is
	// read when the status, the fetch time and the base comparison still hold:
	// the state is only confirmed, so a read of it stays free of git.
	ObserveGitState(ctx context.Context, workspaceID string, st *ports.GitWorktreeStatus) (*domain.GitState, bool, error)
}

var _ StateObserver = (*WorktreeGit)(nil)

func (s *WorktreeGit) ObserveGitState(ctx context.Context, workspaceID string, wt *ports.GitWorktreeStatus) (*domain.GitState, bool, error) {
	ws := s.d.Workspaces.Get(ctx, workspaceID)
	if ws == nil {
		return nil, false, &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	repo, err := s.repository(ctx, ws.RepositoryID)
	if err != nil {
		return nil, false, err
	}
	sl := s.slot(ws.ID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	now := s.d.Now()
	if sl.state != nil && sl.src != nil && sl.src.Equal(wt) && sl.fetched.Equal(s.d.Git.FetchedAt(repo.RootDir)) && now.Sub(sl.baseAt) < baseRecheck {
		sl.state.ReadAt, sl.observed = now.UTC(), true
		return s.named(ctx, sl), false, nil
	}
	st, src, err := s.read(ctx, ws, repo, wt)
	if err != nil {
		return nil, false, err
	}
	changed := sl.state == nil || !sameState(sl.state, st)
	s.hold(sl, st, src, repo)
	sl.observed = true
	return s.named(ctx, sl), changed, nil
}

// named is a copy of the slot's state with its session; the slot is locked.
func (s *WorktreeGit) named(ctx context.Context, sl *stateSlot) *domain.GitState {
	cp := copyState(sl.state)
	if sl.sessionID == "" {
		s.nameSession(ctx, cp)
		sl.sessionID = cp.SessionID
	}
	cp.SessionID = sl.sessionID
	return cp
}

// ObservedStagedPaths are the staged files of a running session's worktree as
// its code watcher last read them, with no git run; false when the watcher does
// not keep the workspace's state current.
func (s *WorktreeGit) ObservedStagedPaths(workspaceID string) ([]string, bool) {
	sl := s.slot(workspaceID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.state == nil || sl.src == nil || !sl.observed || s.d.Now().Sub(sl.state.ReadAt) >= 3*CodeWatchInterval {
		return nil, false
	}
	return slices.Clone(sl.src.StagedPaths), true
}

// sameState: two reads say the same, whenever they were made.
func sameState(a, b *domain.GitState) bool {
	x, y := *a, *b
	x.ReadAt, y.ReadAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(x, y)
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
	origin, err := s.originOf(ctx, repo)
	if err != nil {
		return time.Time{}, err
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

// PushBranch pushes a workspace's branch to origin, and announces the state it
// left. The HEAD is read again under the workspace's lock, so a commit made
// since the person confirmed is never pushed.
func (s *WorktreeGit) PushBranch(ctx context.Context, workspaceID, expectedHead string) (*domain.GitState, error) {
	switch {
	case strings.TrimSpace(workspaceID) == "":
		return nil, invalid("workspace_id is required")
	case strings.TrimSpace(expectedHead) == "":
		return nil, invalid("expected_head is required")
	}
	ws := s.d.Workspaces.Get(ctx, workspaceID)
	if ws == nil {
		return nil, &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	repo, err := s.repository(ctx, ws.RepositoryID)
	if err != nil {
		return nil, err
	}
	if s.d.Fetcher == nil || s.d.Publisher == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "pushing is not available"}
	}
	if !s.d.Files.Exists(ws.Path) {
		return nil, worktreeMissing(ws)
	}
	origin, err := s.originOf(ctx, repo)
	if err != nil {
		return nil, err
	}

	lock := s.pushLock(ws.ID)
	lock.Lock()
	defer lock.Unlock()
	s.forget(ws.ID)
	now, err := s.stateOf(ctx, ws, repo)
	if err != nil {
		return nil, err
	}
	s.nameSession(ctx, now)
	switch {
	case now.Gone:
		return nil, worktreeMissing(ws)
	case now.Branch == "":
		return nil, invalid("the worktree's HEAD is detached; there is no branch to push")
	case now.Head != expectedHead:
		return nil, domain.GitStateError(domain.CodePushStale, "the branch moved since you confirmed: its HEAD is now "+now.Head, now)
	}

	err = s.d.Fetcher.Push(ctx, repo.ID, repo.RootDir, origin, now.Branch)
	s.forget(ws.ID)
	var failed *OriginPushError
	switch {
	case errors.As(err, &failed):
		after := s.reread(ctx, ws, repo, now)
		if failed.Rejected {
			return nil, domain.GitStateError(domain.CodePushRejected, "the remote rejected the push: "+failed.Error(), after)
		}
		return nil, &domain.StructuredError{Code: domain.CodePushFailed, Message: "pushing " + now.Branch + " to origin failed: " + failed.Error()}
	case err != nil:
		return nil, err
	}
	// The push wrote the remote-tracking ref: the counts hold even when the
	// repository was never fetched. A record that cannot be kept only costs
	// the lift, so the push still answers.
	pushedAt := s.d.Now().UTC()
	if err := s.d.Workspaces.SetPushedAt(ws.ID, pushedAt); err != nil {
		slog.Warn("push time not recorded", "workspace", ws.ID, "err", err)
	} else {
		ws.PushedAt = &pushedAt
	}
	s.forget(ws.ID)
	after, err := s.stateOf(ctx, ws, repo)
	if err != nil {
		return nil, err
	}
	s.nameSession(ctx, after)
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{GitState: &ports.GitStateChange{GitState: copyState(after), ProjectID: repo.ProjectID}})
	}
	return after, nil
}

// reread is the workspace's state now, or before when it cannot be read.
func (s *WorktreeGit) reread(ctx context.Context, ws *domain.Workspace, repo *domain.Repository, before *domain.GitState) *domain.GitState {
	st, err := s.stateOf(ctx, ws, repo)
	if err != nil {
		return before
	}
	s.nameSession(ctx, st)
	return st
}

func (s *WorktreeGit) pushLock(workspaceID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.pushLocks[workspaceID]
	if !ok {
		l = &sync.Mutex{}
		s.pushLocks[workspaceID] = l
	}
	return l
}

// originOf is the repository's origin URL, NO_REMOTE when it has none.
func (s *WorktreeGit) originOf(ctx context.Context, repo *domain.Repository) (string, error) {
	origin, err := s.d.Publisher.OriginURL(ctx, repo.RootDir)
	if err != nil {
		return "", err
	}
	if origin == "" {
		return "", &domain.StructuredError{Code: domain.CodeNoRemote, Message: "the repository has no origin remote"}
	}
	return origin, nil
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
		sl.state, sl.src = nil, nil
		sl.mu.Unlock()
	}
}

func copyState(st *domain.GitState) *domain.GitState {
	cp := *st
	return &cp
}

// read computes a workspace's state: from git, or from the status pass wt
// when the caller has one already. It answers the status it used (nil for a
// worktree that is gone).
func (s *WorktreeGit) read(ctx context.Context, ws *domain.Workspace, repo *domain.Repository, wt *ports.GitWorktreeStatus) (*domain.GitState, *ports.GitWorktreeStatus, error) {
	st := &domain.GitState{WorkspaceID: ws.ID, RepositoryID: repo.ID, Branch: ws.Branch}
	fetched := s.d.Git.FetchedAt(repo.RootDir)
	if !fetched.IsZero() {
		st.FetchedAt = &fetched
	}

	var up upstreamCounts
	st.Gone = wt == nil && !s.d.Files.Exists(ws.Path)
	if !st.Gone && wt == nil {
		var err error
		wt, err = s.d.Git.WorktreeStatus(ctx, ws.Path)
		if errs.Code(err) == "WORKSPACE_MISSING" {
			st.Gone, wt = true, nil // removed since the check
		} else if err != nil {
			return nil, nil, err
		}
	}
	if wt != nil {
		st.Branch, st.Head = wt.Branch, wt.Head
		st.Uncommitted = domain.Uncommitted{Tracked: wt.Tracked, Untracked: wt.Untracked, Staged: wt.Staged}
		up = upstreamCounts{ref: wt.Upstream, gone: wt.UpstreamGone, ahead: wt.Ahead, behind: wt.Behind}
		if i := strings.Index(wt.Upstream, "/"); i > 0 {
			up.remote = wt.Upstream[:i]
		}
	}
	if st.Gone {
		bu, err := s.d.Git.BranchUpstream(ctx, repo.RootDir, ws.Branch)
		if err != nil {
			return nil, nil, err
		}
		up = upstreamCounts{ref: bu.Ref, remote: bu.Remote, gone: bu.Gone, ahead: bu.Ahead, behind: bu.Behind}
		if head, err := s.d.Git.ResolveCommit(ctx, repo.RootDir, "refs/heads/"+ws.Branch); err == nil {
			st.Head = head
		}
	}
	if ws.PushedAt != nil {
		pushed := ws.PushedAt.UTC()
		st.PushedAt = &pushed
	}
	st.Upstream = up.state(st.FetchedAt != nil || st.PushedAt != nil)
	st.Base = s.baseOf(ctx, ws, repo.RootDir, st.Branch)
	st.ReadAt = s.d.Now().UTC()
	var src *ports.GitWorktreeStatus
	if wt != nil {
		cp := *wt
		src = &cp
	}
	return st, src, nil
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
		return domain.BaseState{Ref: ref}
	}
	ahead, behind, err := s.d.Git.Divergence(ctx, root, ref, branch)
	if err != nil {
		return domain.BaseState{Ref: ref, Gone: true}
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
