package workspacestest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// NewWorktreeGit records the fixture's repository and one workspace per branch
// in a fresh database and serves them through workspaces.WorktreeGit over real
// git and the real filesystem.
func NewWorktreeGit(t *testing.T, f *portstest.WorktreeGitFixture) (*workspaces.WorktreeGit, portstest.WorktreeGitTarget) {
	t.Helper()
	svc, at, _ := NewWorktreeGitWithFeed(t, f)
	return svc, at
}

// FeedRecorder keeps what a service announced on the project feed.
type FeedRecorder struct {
	mu      sync.Mutex
	changes []ports.ProjectChange
}

func (r *FeedRecorder) AnnounceChange(c ports.ProjectChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changes = append(r.changes, c)
}

// Kinds names what was announced so far, in order: "code_changed" or
// "git_state".
func (r *FeedRecorder) Kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.changes {
		switch {
		case c.CodeChanged != nil:
			out = append(out, "code_changed")
		case c.GitState != nil:
			out = append(out, "git_state")
		}
	}
	return out
}

// GitStates are the git states announced so far, in order.
func (r *FeedRecorder) GitStates() []ports.GitStateChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ports.GitStateChange
	for _, c := range r.changes {
		if c.GitState != nil {
			out = append(out, *c.GitState)
		}
	}
	return out
}

// NewWorktreeGitWithFeed is NewWorktreeGit with the feed it announces on, and
// the shared origin fetch a pull request refresh would run.
func NewWorktreeGitWithFeed(t *testing.T, f *portstest.WorktreeGitFixture, opts ...WorktreeGitOption) (*workspaces.WorktreeGit, portstest.WorktreeGitTarget, *FeedRecorder) {
	t.Helper()
	o := worktreeGitOptions{publisher: gitcli.NewBranchPublisher(), timeout: workspaces.OriginFetchTimeout, reader: func(g *gitcli.Code) ports.GitWorktreeReader { return g }}
	for _, opt := range opts {
		opt(&o)
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create("git-project", "fixture", "", f.OriginDir, f.RepoDir)
	if err != nil {
		t.Fatal(err)
	}
	alone, err := repos.Create("git-project", "alone", "", "https://example.com/alone.git", f.NoOriginDir)
	if err != nil {
		t.Fatal(err)
	}
	wsRepo := sqlite.NewWorkspaceRepository(db)
	aloneWS, err := wsRepo.Create(domain.Workspace{RepositoryID: alone.ID, Name: "alone", Branch: "main", Path: f.NoOriginDir})
	if err != nil {
		t.Fatal(err)
	}
	at := portstest.WorktreeGitTarget{RepositoryID: repo.ID, NoRemoteRepositoryID: alone.ID, NoRemoteWorkspaceID: aloneWS.ID, Workspaces: map[string]string{}, Sessions: map[string]string{}}
	for _, branch := range portstest.WorktreeGitBranches {
		ws, err := wsRepo.Create(domain.Workspace{RepositoryID: repo.ID, Name: branch, Branch: branch, Path: f.Worktrees[branch], BaseRef: f.BaseRef})
		if err != nil {
			t.Fatal(err)
		}
		at.Workspaces[branch] = ws.ID
	}
	// One task with a session on each workspace, started in turn.
	tickets := sqlite.NewTicketRepository(db)
	task, err := tickets.Create("git-project", "task", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	at.TaskID = task.ID
	sessions := sqlite.NewSessionRepository(db)
	for _, branch := range portstest.WorktreeGitBranches {
		sess, err := sessions.Create(&domain.Session{
			ProjectID: "git-project", RepositoryID: repo.ID, TicketID: task.ID, Status: domain.SessionRunning,
			WorkspaceID: at.Workspaces[branch], Branch: branch,
		})
		if err != nil {
			t.Fatal(err)
		}
		at.Sessions[branch] = sess.ID
	}
	// A session with no workspace is not listed.
	if _, err := sessions.Create(&domain.Session{ProjectID: "git-project", RepositoryID: repo.ID, TicketID: task.ID, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	listed, err := sessions.Query(context.Background(), ports.SessionFilter{TicketID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, sess := range listed {
		if sess.WorkspaceID != "" {
			at.TaskOrder = append(at.TaskOrder, sess.Branch)
		}
	}
	clock := &passingClock{now: time.Now()}
	feed := &FeedRecorder{}
	code := gitcli.NewCode()
	svc := workspaces.NewWorktreeGit(workspaces.WorktreeGitDeps{
		Workspaces: wsRepo, Repositories: repos, Sessions: sessions, Tickets: tickets,
		Git: o.reader(code), Files: filesystem.NewWorktreeFiles(), Now: clock.Now,
		Publisher: o.publisher, Fetcher: workspaces.NewOriginFetcher(o.publisher, gitcredentialstest.New()).WithTimeout(o.timeout), Feed: feed, Pushes: o.pushes,
	})
	at.PassInterval = clock.Pass
	if o.codeOut != nil {
		counting := &countingStaged{GitCode: code}
		o.codeOut.StagedRuns = &counting.runs
		o.codeOut.Code = workspaces.NewCode(workspaces.CodeDeps{
			Workspaces: wsRepo, Repositories: repos, Git: counting, Files: filesystem.NewWorktreeFiles(), Staged: svc, Now: clock.Now,
		})
	}
	if o.watched != nil {
		// Every session runs; someone follows the project's feed.
		wclock := &passingClock{now: time.Now()}
		w := workspaces.NewCodeWatcher(workspaces.CodeWatcherDeps{
			Sessions: sessions, Workspaces: wsRepo, Git: code, Feed: feed, State: svc, Now: wclock.Now,
		})
		for _, id := range at.Sessions {
			w.Track(id)
		}
		w.ProjectFollowers("git-project", 1)
		o.watched.Poll = func() { wclock.Pass(); w.Poll(context.Background()) }
		o.watched.PollNow = func() { w.Poll(context.Background()) }
	}
	return svc, at, feed
}

// CodeHandle is a code browser over the fixture's workspaces, sharing the
// worktree git state's observations.
type CodeHandle struct {
	Code *workspaces.Code
	// StagedRuns counts the git runs that listed a worktree's staged files.
	StagedRuns *atomic.Int64
}

type countingStaged struct {
	ports.GitCode
	runs atomic.Int64
}

func (c *countingStaged) StagedPaths(ctx context.Context, dir string) ([]string, error) {
	c.runs.Add(1)
	return c.GitCode.StagedPaths(ctx, dir)
}

// WithCode builds a code browser beside the service and hands it to h.
func WithCode(h *CodeHandle) WorktreeGitOption {
	return func(o *worktreeGitOptions) { o.codeOut = h }
}

// Watched drives a code watcher built over the fixture's sessions.
type Watched struct {
	// Poll looks at every worktree once, an interval after the last look;
	// PollNow looks without letting any time pass.
	Poll, PollNow func()
}

type worktreeGitOptions struct {
	publisher ports.BranchPublisher
	timeout   time.Duration
	reader    func(*gitcli.Code) ports.GitWorktreeReader
	watched   *Watched
	codeOut   *CodeHandle
	pushes    ports.RecordedPushStore
}

// WithPushes keeps the person's pushes in store.
func WithPushes(store ports.RecordedPushStore) WorktreeGitOption {
	return func(o *worktreeGitOptions) { o.pushes = store }
}

// WithCodeWatcher builds a code watcher over the fixture's running sessions,
// announcing on the same feed and keeping the same states, and hands its
// controls to w.
func WithCodeWatcher(w *Watched) WorktreeGitOption {
	return func(o *worktreeGitOptions) { o.watched = w }
}

// WithGitReader serves the git state's reads through wrap(real git), so a
// test can count them.
func WithGitReader(wrap func(ports.GitWorktreeReader) ports.GitWorktreeReader) WorktreeGitOption {
	return func(o *worktreeGitOptions) {
		o.reader = func(g *gitcli.Code) ports.GitWorktreeReader { return wrap(g) }
	}
}

// WorktreeGitOption changes how NewWorktreeGitWithFeed is built.
type WorktreeGitOption func(*worktreeGitOptions)

// WithPublisher serves fetch and push through p instead of real git.
func WithPublisher(p ports.BranchPublisher) WorktreeGitOption {
	return func(o *worktreeGitOptions) { o.publisher = p }
}

// WithRemoteTimeout bounds each fetch and push to d.
func WithRemoteTimeout(d time.Duration) WorktreeGitOption {
	return func(o *worktreeGitOptions) { o.timeout = d }
}
