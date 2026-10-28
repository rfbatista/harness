package workspacestest

import (
	"context"
	"sync"
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
func NewWorktreeGitWithFeed(t *testing.T, f *portstest.WorktreeGitFixture) (*workspaces.WorktreeGit, portstest.WorktreeGitTarget, *FeedRecorder) {
	t.Helper()
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
	at := portstest.WorktreeGitTarget{RepositoryID: repo.ID, NoRemoteRepositoryID: alone.ID, Workspaces: map[string]string{}, Sessions: map[string]string{}}
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
	publisher := gitcli.NewBranchPublisher()
	svc := workspaces.NewWorktreeGit(workspaces.WorktreeGitDeps{
		Workspaces: wsRepo, Repositories: repos, Sessions: sessions, Tickets: tickets,
		Git: gitcli.NewCode(), Files: filesystem.NewWorktreeFiles(), Now: clock.Now,
		Publisher: publisher, Fetcher: workspaces.NewOriginFetcher(publisher, gitcredentialstest.New()), Feed: feed,
	})
	at.PassInterval = clock.Pass
	return svc, at, feed
}
