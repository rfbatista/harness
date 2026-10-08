package workspaces

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.PullRequests = (*PullRequests)(nil)

const (
	// RefreshEvery is how often, at most, a workspace's pull request is
	// looked up on its host after a read.
	RefreshEvery = 30 * time.Second

	pushTimeout    = 90 * time.Second
	fetchTimeout   = 30 * time.Second
	hostTimeout    = 20 * time.Second
	maxBodyCommits = 50
)

// PullRequestDeps is what the pull request use case needs. Sessions and
// Tickets (the task a workspace's session works on, for the suggested title
// and body) and Feed may be nil.
type PullRequestDeps struct {
	Workspaces   ports.WorkspaceRepository
	Repositories ports.RepositoryRepository
	History      ports.GitHistory
	Git          ports.BranchPublisher
	Host         ports.CodeHost
	Store        ports.PullRequestRepository
	Sessions     ports.SessionRepository
	Tickets      ports.TicketReader
	Feed         ports.ProjectChangeSink
	// Now is the clock the refresh rate is measured with; nil is time.Now.
	Now func() time.Time
}

// PullRequests opens a workspace's branch as a pull request on its origin.
// It is apart from Service because it announces on the project feed, which
// the orchestration owns, and the orchestration is built from Service.
type PullRequests struct {
	d PullRequestDeps

	// Reads ask the host in the background, under ctx, which Close ends.
	ctx    context.Context
	cancel context.CancelFunc
	bg     sync.WaitGroup

	mu        sync.Mutex
	closed    bool
	locks     map[string]*sync.Mutex // per workspace: one create or refresh at a time
	refreshed map[string]time.Time   // per workspace: when the host was last asked
}

// NewPullRequests returns the use case.
func NewPullRequests(d PullRequestDeps) *PullRequests {
	if d.Now == nil {
		d.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &PullRequests{d: d, ctx: ctx, cancel: cancel, locks: map[string]*sync.Mutex{}, refreshed: map[string]time.Time{}}
}

// Subscribe removes a deleted project's pull request records. A deleted
// workspace keeps them.
func (s *PullRequests) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.ProjectDeleted) error {
		return s.d.Store.DeleteByProject(ev.ProjectID)
	})
}

// Close ends the background host lookups and waits for them.
func (s *PullRequests) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.bg.Wait()
}

// Wait returns when the background host lookups started so far are done.
func (s *PullRequests) Wait() { s.bg.Wait() }

// target is a workspace with what its pull request is opened against.
type target struct {
	ws        *domain.Workspace
	projectID string
	root      string
	remote    *domain.GitRemote // nil: no origin, or one that is no URL
	base      string            // default base branch
}

func (t target) github() bool { return t.remote != nil && t.remote.IsGitHub() }

func (s *PullRequests) resolve(ctx context.Context, workspaceID string) (*target, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, domain.PullRequestError("INVALID_INPUT", "workspace_id is required")
	}
	ws := s.d.Workspaces.Get(workspaceID)
	if ws == nil {
		return nil, domain.PullRequestError("WORKSPACE_NOT_FOUND", "workspace not found")
	}
	repo := s.d.Repositories.Get(ws.RepositoryID)
	if repo == nil {
		return nil, domain.PullRequestError("REPOSITORY_NOT_FOUND", "repository not found")
	}
	if repo.RootDir == "" {
		return nil, domain.PullRequestError("INVALID_ROOT", "repository has no root_dir")
	}
	t := &target{ws: ws, projectID: repo.ProjectID, root: repo.RootDir}
	url, err := s.d.Git.OriginURL(ctx, t.root)
	if err != nil {
		return nil, err
	}
	if r, ok := domain.ParseGitRemote(url); ok {
		t.remote = &r
	}
	t.base = s.defaultBase(ctx, t.root, ws.BaseRef)
	return t, nil
}

// defaultBase is the branch the workspace was cut from, or, when that is no
// branch (empty, a commit, a tag), the branch checked out in the repository.
func (s *PullRequests) defaultBase(ctx context.Context, root, baseRef string) string {
	if b := domain.BranchFromRef(baseRef); b != "" &&
		(s.d.Git.HasRef(ctx, root, "refs/heads/"+b) || s.d.Git.HasRef(ctx, root, "refs/remotes/origin/"+b)) {
		return b
	}
	if st, err := s.d.History.Status(ctx, root); err == nil {
		return st.Branch
	}
	return ""
}

// compareRef is what the head is compared with: origin's base as last
// fetched, else the local base; "" when neither resolves.
func (s *PullRequests) compareRef(ctx context.Context, root, base string) (ref string, onOrigin bool) {
	if base == "" {
		return "", false
	}
	if r := "refs/remotes/origin/" + base; s.d.Git.HasRef(ctx, root, r) {
		return r, true
	}
	if r := "refs/heads/" + base; s.d.Git.HasRef(ctx, root, r) {
		return r, false
	}
	return "", false
}

func (s *PullRequests) ahead(ctx context.Context, root, ref, head string) int {
	if ref == "" {
		return 0
	}
	n, _, err := s.d.History.Divergence(ctx, root, ref, "refs/heads/"+head)
	if err != nil {
		return 0
	}
	return n
}

// PullRequest answers from git and the stored record only, then asks the
// host in the background; what it finds changed arrives on the feed.
func (s *PullRequests) PullRequest(ctx context.Context, workspaceID string) (*ports.PullRequestView, error) {
	t, err := s.resolve(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	stored := s.d.Store.LatestForWorkspace(t.ws.ID)
	draft := s.draft(ctx, t, stored)
	s.refreshLater(t, stored)
	return &ports.PullRequestView{Draft: draft, PullRequest: stored}, nil
}

func (s *PullRequests) draft(ctx context.Context, t *target, stored *domain.PullRequest) domain.PullRequestDraft {
	d := domain.PullRequestDraft{
		WorkspaceID: t.ws.ID, RepositoryID: t.ws.RepositoryID, HeadBranch: t.ws.Branch, BaseBranch: t.base,
		Remote: t.remote, Blockers: []string{},
	}
	ref, onOrigin := s.compareRef(ctx, t.root, t.base)
	d.Ahead = s.ahead(ctx, t.root, ref, t.ws.Branch)
	if st, err := s.d.History.Status(ctx, t.ws.Path); err == nil {
		d.UncommittedChanges = len(st.Changes)
	}
	var subjects []string
	if ref != "" {
		subjects, _ = s.d.Git.Subjects(ctx, t.root, ref, "refs/heads/"+t.ws.Branch, maxBodyCommits)
	}
	d.Title, d.Body = s.suggest(ctx, t, subjects)

	if !t.github() {
		d.Blockers = append(d.Blockers, domain.CodePRNoRemote)
	}
	if err := s.d.Host.Ready(ctx); err != nil {
		d.Blockers = append(d.Blockers, domain.CodePRToolUnavailable)
	}
	if stored != nil && !stored.Closed() {
		d.Blockers = append(d.Blockers, domain.CodePRAlreadyExists)
	}
	if t.remote != nil && !onOrigin {
		d.Blockers = append(d.Blockers, domain.CodePRBaseNotFound)
	}
	if ref != "" && d.Ahead == 0 {
		d.Blockers = append(d.Blockers, domain.CodePRNoCommits)
	}
	return d
}

// suggest is the title (the task's, else the newest commit subject, else the
// branch) and the body (the task's description, then the commits as a list).
func (s *PullRequests) suggest(ctx context.Context, t *target, subjects []string) (title, body string) {
	var description string
	if task := s.task(ctx, t); task != nil {
		title, description = task.Title, strings.TrimSpace(task.Description)
	}
	if title == "" && len(subjects) > 0 {
		title = subjects[0]
	}
	if title == "" {
		title = t.ws.Branch
	}
	var b strings.Builder
	b.WriteString(description)
	for i, subject := range subjects {
		if i == 0 && description != "" {
			b.WriteString("\n\n")
		} else if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- " + subject)
	}
	return title, b.String()
}

// task is the task of the session running in the workspace, if any.
func (s *PullRequests) task(ctx context.Context, t *target) *domain.Ticket {
	if s.d.Sessions == nil || s.d.Tickets == nil {
		return nil
	}
	for _, sess := range s.d.Sessions.List(ports.SessionFilter{ProjectID: t.projectID}) {
		if sess.WorkspaceID != t.ws.ID || sess.TicketID == "" {
			continue
		}
		if tk, err := s.d.Tickets.GetTicket(ctx, sess.TicketID); err == nil {
			return tk
		}
	}
	return nil
}

// refreshLater asks the host, at most once per workspace every RefreshEvery,
// about a stored open pull request, or about an open one to adopt for a
// pushed branch with nothing stored.
func (s *PullRequests) refreshLater(t *target, stored *domain.PullRequest) {
	if !t.github() {
		return
	}
	switch {
	case stored != nil && stored.Closed():
		return
	case stored == nil && !s.d.Git.HasRef(s.ctx, t.root, "refs/remotes/origin/"+t.ws.Branch):
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.d.Now()
	if s.closed {
		return
	}
	if last, ok := s.refreshed[t.ws.ID]; ok && now.Sub(last) < RefreshEvery {
		return
	}
	s.refreshed[t.ws.ID] = now
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		ctx, cancel := context.WithTimeout(s.ctx, hostTimeout)
		defer cancel()
		lock := s.lock(t.ws.ID)
		lock.Lock()
		defer lock.Unlock()
		// Host errors are ignored here: the next read tries again.
		if stored == nil {
			if found, err := s.d.Host.FindOpen(ctx, *t.remote, t.ws.Branch); err == nil && found != nil &&
				s.d.Store.LatestForWorkspace(t.ws.ID) == nil {
				_, _ = s.store(t, found)
			}
			return
		}
		_, _ = s.refresh(ctx, t, stored)
	}()
}

// refresh reads a stored pull request from the host, storing and announcing
// it when it changed.
func (s *PullRequests) refresh(ctx context.Context, t *target, stored *domain.PullRequest) (*domain.PullRequest, error) {
	got, err := s.d.Host.Get(ctx, *t.remote, stored.Number)
	if err != nil {
		return stored, err
	}
	if got.State == stored.State && got.Title == stored.Title && got.Draft == stored.Draft && got.Base == stored.BaseBranch {
		return stored, nil
	}
	return s.store(t, got)
}

// store records the host's pull request against the workspace and announces it.
func (s *PullRequests) store(t *target, h *ports.HostPullRequest) (*domain.PullRequest, error) {
	pr, err := s.d.Store.Save(&domain.PullRequest{
		WorkspaceID: t.ws.ID, RepositoryID: t.ws.RepositoryID, ProjectID: t.projectID,
		Number: h.Number, URL: h.URL, Title: h.Title, HeadBranch: h.Head, BaseBranch: h.Base,
		Draft: h.Draft, State: h.State, CreatedAt: h.CreatedAt,
	})
	if err != nil {
		return nil, err
	}
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{PullRequest: pr})
	}
	return pr, nil
}

func (s *PullRequests) lock(workspaceID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[workspaceID]
	if !ok {
		l = &sync.Mutex{}
		s.locks[workspaceID] = l
	}
	return l
}

func alreadyExists(pr *domain.PullRequest) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.CodePRAlreadyExists,
			Message: domain.CodePRAlreadyExists + ": pull request #" + strconv.Itoa(pr.Number) + " is already open for " + pr.HeadBranch + ": " + pr.URL},
		Details: map[string]any{"pull_request": pr},
	}
}

// CreatePullRequest pushes the workspace's branch to origin and opens its
// pull request, one at a time per workspace. It commits nothing and never
// forces; an open pull request already on the host for the branch is adopted
// and answered as PR_ALREADY_EXISTS.
func (s *PullRequests) CreatePullRequest(ctx context.Context, req ports.CreatePullRequest) (*domain.PullRequest, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, domain.PullRequestError("INVALID_INPUT", "a title is required")
	}
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return nil, domain.PullRequestError("INVALID_INPUT", "workspace_id is required")
	}
	lock := s.lock(req.WorkspaceID)
	lock.Lock()
	defer lock.Unlock()

	t, err := s.resolve(ctx, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	switch {
	case t.remote == nil:
		return nil, domain.PullRequestError(domain.CodePRNoRemote, "the repository has no origin remote on GitHub")
	case !t.remote.IsGitHub():
		return nil, domain.PullRequestError(domain.CodePRNoRemote, "origin is on "+t.remote.Host+", not GitHub; pull requests are opened on GitHub only")
	}
	if err := s.d.Host.Ready(ctx); err != nil {
		return nil, err
	}
	if err := s.refuseOpen(ctx, t); err != nil {
		return nil, err
	}

	base := strings.TrimSpace(req.BaseBranch)
	if base == "" {
		base = t.base
	}
	if base == "" {
		return nil, domain.PullRequestError(domain.CodePRBaseNotFound, "no base branch: the repository has no branch checked out")
	}
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	err = s.d.Git.FetchBranch(fetchCtx, t.root, base)
	cancel()
	switch {
	case errors.Is(err, ports.ErrRemoteBranchNotFound):
		return nil, domain.PullRequestError(domain.CodePRBaseNotFound, "the base branch "+base+" does not exist on origin")
	case err != nil:
		return nil, domain.PullRequestError(domain.CodePRPushFailed, "reading "+base+" from origin failed: "+err.Error())
	}
	if s.ahead(ctx, t.root, "refs/remotes/origin/"+base, t.ws.Branch) == 0 {
		return nil, domain.PullRequestError(domain.CodePRNoCommits, t.ws.Branch+" has no commits that "+base+" on origin lacks")
	}

	pushCtx, cancel := context.WithTimeout(ctx, pushTimeout)
	err = s.d.Git.Push(pushCtx, t.root, t.ws.Branch)
	cancel()
	if err != nil {
		return nil, domain.PullRequestError(domain.CodePRPushFailed, "pushing "+t.ws.Branch+" to origin failed: "+err.Error())
	}

	hostCtx, cancel := context.WithTimeout(ctx, hostTimeout)
	defer cancel()
	opened, err := s.d.Host.Open(hostCtx, *t.remote, ports.OpenPullRequest{Head: t.ws.Branch, Base: base, Title: title, Body: req.Body, Draft: req.Draft})
	switch {
	case errors.Is(err, ports.ErrPullRequestExists):
		if found, ferr := s.d.Host.FindOpen(hostCtx, *t.remote, t.ws.Branch); ferr == nil && found != nil {
			pr, serr := s.store(t, found)
			if serr != nil {
				return nil, serr
			}
			return nil, alreadyExists(pr)
		}
		return nil, domain.PullRequestError(domain.CodePRCreateFailed, err.Error())
	case errs.Code(err) != "":
		return nil, err
	case err != nil:
		return nil, domain.PullRequestError(domain.CodePRCreateFailed, "opening the pull request on GitHub failed: "+err.Error())
	}
	return s.store(t, opened)
}

// refuseOpen is PR_ALREADY_EXISTS when the branch has an open pull request:
// one on the host (adopted), or the stored one if the host still has it open.
// A stored one the host has since closed or merged is stored as such.
func (s *PullRequests) refuseOpen(ctx context.Context, t *target) error {
	hostCtx, cancel := context.WithTimeout(ctx, hostTimeout)
	defer cancel()
	found, err := s.d.Host.FindOpen(hostCtx, *t.remote, t.ws.Branch)
	if err != nil {
		if errs.Code(err) != "" {
			return err
		}
		return domain.PullRequestError(domain.CodePRCreateFailed, "reading the branch's pull requests on GitHub failed: "+err.Error())
	}
	if found != nil {
		pr, err := s.store(t, found)
		if err != nil {
			return err
		}
		return alreadyExists(pr)
	}
	stored := s.d.Store.LatestForWorkspace(t.ws.ID)
	if stored == nil || stored.Closed() {
		return nil
	}
	if fresh, err := s.refresh(hostCtx, t, stored); err != nil || !fresh.Closed() {
		return alreadyExists(stored)
	}
	return nil
}
