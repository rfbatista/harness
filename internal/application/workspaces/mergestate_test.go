package workspaces_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// mergeFixture is a project whose repository is a real git checkout on main,
// with tasks read through planning decorated by the merge states.
type mergeFixture struct {
	t         *testing.T
	ctx       context.Context
	db        *gorm.DB
	plan      *planning.Service
	merges    *workspaces.MergeStates
	projectID string
	repoID    string
	repoDir   string
	worktrees map[string]string // by branch
	wsIDs     map[string]string // workspace by branch
	feed      *ticketFeed
	clock     *fakeClock
}

func newMergeFixture(t *testing.T, defaultBranch string) *mergeFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch="+defaultBranch, "init")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "init")
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create(p.ID, "app", "", "https://example.com/app.git", dir)
	if err != nil {
		t.Fatal(err)
	}
	tickets := sqlite.NewTicketRepository(db)
	plan := planning.NewService(tickets, sqlite.NewDocumentRepository(db), projects)
	feed, clock := &ticketFeed{}, &fakeClock{now: time.Now()}
	merges := workspaces.NewMergeStates(workspaces.MergeStateDeps{
		Sessions: sqlite.NewSessionRepository(db), Workspaces: sqlite.NewWorkspaceRepository(db),
		Repositories: repos, Git: gitcli.NewBranchMerges(),
		Tickets: tickets, Announcer: plan, Now: clock.Now,
	})
	t.Cleanup(merges.Close)
	plan.Decorator, plan.Announcer, plan.MergeRefresher = merges, feed, merges
	return &mergeFixture{t: t, ctx: context.Background(), db: db, plan: plan, merges: merges, projectID: p.ID, repoID: repo.ID, repoDir: dir,
		worktrees: map[string]string{}, wsIDs: map[string]string{}, feed: feed, clock: clock}
}

// ticketFeed is the project feed's ticket announcements.
type ticketFeed struct {
	mu      sync.Mutex
	tickets []domain.Ticket
}

func (f *ticketFeed) AnnounceTicket(tk *domain.Ticket, _ bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tickets = append(f.tickets, *tk)
}

// since is what was announced of the task after the first n announcements.
func (f *ticketFeed) since(n int, taskID string) []domain.Ticket {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Ticket
	for _, tk := range f.tickets[n:] {
		if tk.ID == taskID {
			out = append(out, tk)
		}
	}
	return out
}

func (f *ticketFeed) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tickets)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// gitRun runs git in dir with a fixed identity, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@test"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func (f *mergeFixture) task(status domain.TicketStatus) string {
	f.t.Helper()
	tk, err := f.plan.CreateTicket(f.ctx, f.projectID, "task", "", status)
	if err != nil {
		f.t.Fatal(err)
	}
	return tk.ID
}

// session gives the task a session whose worktree holds a new branch cut
// from base, with commits commits of its own.
func (f *mergeFixture) session(taskID, branch, base string, commits int) {
	f.t.Helper()
	path := filepath.Join(f.t.TempDir(), branch)
	gitRun(f.t, f.repoDir, "worktree", "add", "-b", branch, path, base)
	f.worktrees[branch] = path
	for i := range commits {
		name := filepath.Join(path, branch+".txt")
		if err := os.WriteFile(name, []byte{byte('a' + i)}, 0o644); err != nil {
			f.t.Fatal(err)
		}
		gitRun(f.t, path, "add", ".")
		gitRun(f.t, path, "commit", "-m", branch)
	}
	ws, err := sqlite.NewWorkspaceRepository(f.db).Create(domain.Workspace{RepositoryID: f.repoID, Name: branch, Branch: branch, Path: path, BaseRef: base})
	if err != nil {
		f.t.Fatal(err)
	}
	f.wsIDs[branch] = ws.ID
	_, err = sqlite.NewSessionRepository(f.db).Create(&domain.Session{
		ProjectID: f.projectID, TicketID: taskID, RepositoryID: f.repoID, WorkspaceID: ws.ID, Branch: branch,
		Status: domain.SessionDone, CreatedAt: time.Now(),
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// merge merges branch into the checkout's current branch, fast-forwarding
// when it can.
func (f *mergeFixture) merge(branch string) {
	f.t.Helper()
	gitRun(f.t, f.repoDir, "merge", "--no-edit", branch)
}

// deleteBranch removes the branch's worktree and the branch.
func (f *mergeFixture) deleteBranch(branch string) {
	f.t.Helper()
	gitRun(f.t, f.repoDir, "worktree", "remove", "--force", f.worktrees[branch])
	gitRun(f.t, f.repoDir, "branch", "-D", branch)
}

func (f *mergeFixture) read(taskID string) *domain.MergeState {
	f.t.Helper()
	tk, err := f.plan.GetTicket(f.ctx, taskID)
	if err != nil {
		f.t.Fatal(err)
	}
	return tk.MergeState
}

func wantMerge(t *testing.T, got *domain.MergeState, want domain.MergeState) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("merge state = %+v, want %+v", got, want)
	}
}

func TestAReviewTaskReadsHowManyOfItsBranchesAreMerged(t *testing.T) {
	for _, c := range []struct {
		name   string
		merged int
		want   domain.MergeState
	}{
		{"both merged", 2, domain.MergeState{State: domain.MergeMerged, Merged: 2, Total: 2}},
		{"one merged", 1, domain.MergeState{State: domain.MergePartial, Merged: 1, Total: 2}},
		{"none merged", 0, domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newMergeFixture(t, "main")
			id := f.task(domain.TicketStatusReview)
			f.session(id, "feat-a", "main", 1)
			f.session(id, "feat-b", "main", 2)
			// The first merge fast-forwards main onto feat-a: a merged branch
			// still counts though nothing is left ahead of its base.
			for _, b := range []string{"feat-a", "feat-b"}[:c.merged] {
				f.merge(b)
			}
			wantMerge(t, f.read(id), c.want)
		})
	}
}

func TestADoneTaskCarriesItsMergeStateToo(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusDone)
	f.session(id, "feat", "main", 1)
	f.merge("feat")
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

func TestATaskWhoseSessionsCommittedNothingReadsNone(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "idle", "main", 0)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeNone})

	bare := f.task(domain.TicketStatusReview)
	wantMerge(t, f.read(bare), domain.MergeState{State: domain.MergeNone})
}

func TestDeletingACountedBranchMakesTheMergeStateUnknown(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat-a", "main", 1)
	f.session(id, "feat-b", "main", 1)
	f.merge("feat-a")
	f.deleteBranch("feat-b")
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeUnknown})
}

func TestARepositoryWithoutADefaultBranchReadsUnknown(t *testing.T) {
	f := newMergeFixture(t, "trunk")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "trunk", 1)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeUnknown})
}

func TestOriginHEADNamesTheDefaultBranch(t *testing.T) {
	f := newMergeFixture(t, "trunk")
	// main exists but is not the default: origin/HEAD says trunk is.
	gitRun(t, f.repoDir, "branch", "main")
	gitRun(t, f.repoDir, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	gitRun(t, f.repoDir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "trunk", 1)
	f.merge("feat") // into the local trunk, not pushed
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

func TestOnlyReviewAndDoneTasksOnTheBoardCarryAMergeState(t *testing.T) {
	f := newMergeFixture(t, "main")
	working := f.task(domain.TicketStatusInProgress)
	f.session(working, "feat-w", "main", 1)
	if got := f.read(working); got != nil {
		t.Fatalf("in_progress merge state = %+v, want null", got)
	}

	archived := f.task(domain.TicketStatusDone)
	f.session(archived, "feat-d", "main", 1)
	if err := f.db.Model(&sqlite.TicketModel{}).Where("id = ?", archived).Update("archived_at", time.Now().UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if got := f.read(archived); got != nil {
		t.Fatalf("archived done merge state = %+v, want null", got)
	}
}

func TestASecondReadServesTheCachedMergeStateUntilItIsRefreshed(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	notMerged := domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1}
	wantMerge(t, f.read(id), notMerged)

	f.merge("feat")
	wantMerge(t, f.read(id), notMerged)
	list, err := f.plan.ListTickets(f.ctx, f.projectID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	wantMerge(t, list[0].MergeState, notMerged)

	if _, err := f.merges.Refresh(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

// commit adds a commit to the branch's worktree.
func (f *mergeFixture) commit(branch string) {
	f.t.Helper()
	path := f.worktrees[branch]
	if err := os.WriteFile(filepath.Join(path, "more.txt"), []byte(time.Now().String()), 0o644); err != nil {
		f.t.Fatal(err)
	}
	gitRun(f.t, path, "add", ".")
	gitRun(f.t, path, "commit", "-m", "more")
}

// codeChanged signals that the branch's workspace changed code, as the code
// watcher does on the project feed.
func (f *mergeFixture) codeChanged(branch string) {
	f.merges.AnnounceChange(ports.ProjectChange{CodeChanged: &ports.CodeChange{WorkspaceID: f.wsIDs[branch], ProjectID: f.projectID}})
}

// announced waits for the feed to announce the task with want after the
// first n announcements.
func (f *mergeFixture) announced(n int, taskID string, want domain.MergeState) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := f.feed.since(n, taskID)
		for _, tk := range got {
			if tk.MergeState != nil && *tk.MergeState == want {
				return
			}
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("announced %+v, want merge state %+v", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// quiet waits for the background refreshes and fails if anything about the
// task was announced after the first n announcements.
func (f *mergeFixture) quiet(n int, taskID string) {
	f.t.Helper()
	f.merges.Wait()
	if got := f.feed.since(n, taskID); len(got) != 0 {
		f.t.Fatalf("announced %+v, want nothing", got)
	}
}

func TestACodeChangeAnnouncesAReviewTaskWhoseBranchWasMerged(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1})

	f.merge("feat")
	n := f.feed.len()
	f.codeChanged("feat")
	f.announced(n, id, domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})

	n = f.feed.len()
	f.codeChanged("feat")
	f.quiet(n, id)
}

func TestANewCommitOnAMergedBranchAnnouncesItIsNoLongerAllMerged(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat-a", "main", 1)
	f.session(id, "feat-b", "main", 1)
	f.merge("feat-a")
	f.merge("feat-b")
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 2, Total: 2})

	f.commit("feat-b")
	n := f.feed.len()
	f.codeChanged("feat-b")
	f.announced(n, id, domain.MergeState{State: domain.MergePartial, Merged: 1, Total: 2})

	f.commit("feat-a")
	n = f.feed.len()
	f.codeChanged("feat-a")
	f.announced(n, id, domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 2})
}

func TestACodeChangeOfATaskNotInReviewOrDoneAnnouncesNothing(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusInProgress)
	f.session(id, "feat", "main", 1)
	f.merge("feat")
	n := f.feed.len()
	f.codeChanged("feat")
	f.quiet(n, id)
}

func TestAPullRequestStateChangeRecomputesTheMergeState(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1})
	pr := &domain.PullRequest{WorkspaceID: f.wsIDs["feat"], ProjectID: f.projectID, Number: 1, State: domain.PullRequestOpen}

	n := f.feed.len()
	f.merges.AnnounceChange(ports.ProjectChange{PullRequest: pr})
	f.quiet(n, id)

	f.merge("feat")
	pr.State = domain.PullRequestMerged
	f.merges.AnnounceChange(ports.ProjectChange{PullRequest: pr})
	f.announced(n, id, domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

func TestMovingATaskIntoReviewAnnouncesItWithItsFreshMergeState(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1})
	move := func(to domain.TicketStatus) *domain.Ticket {
		t.Helper()
		tk, err := f.plan.PatchTicket(f.ctx, id, ports.TicketPatch{Status: &to})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	move(domain.TicketStatusInProgress)
	f.merge("feat")

	n := f.feed.len()
	tk := move(domain.TicketStatusReview)
	merged := domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1}
	wantMerge(t, tk.MergeState, merged)
	got := f.feed.since(n, id)
	if len(got) == 0 {
		t.Fatal("the move was not announced")
	}
	wantMerge(t, got[0].MergeState, merged)
}

func TestLoadingTheBoardServesAStaleMergeStateThenAnnouncesTheRefreshedOne(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	notMerged := domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1}
	merged := domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1}
	wantMerge(t, f.read(id), notMerged)
	board := func() *domain.MergeState {
		t.Helper()
		list, err := f.plan.ListTickets(f.ctx, f.projectID)
		if err != nil || len(list) != 1 {
			t.Fatalf("list = %v, %v", list, err)
		}
		return list[0].MergeState
	}

	f.merge("feat")
	f.clock.advance(workspaces.MergeStateFreshFor + time.Second)
	n := f.feed.len()
	wantMerge(t, board(), notMerged)
	f.announced(n, id, merged)

	n = f.feed.len()
	wantMerge(t, board(), merged)
	f.quiet(n, id)

	f.clock.advance(workspaces.MergeStateFreshFor + time.Second)
	wantMerge(t, board(), merged)
	f.quiet(n, id)
}

func TestLoadingTheBoardComputesUnreadMergeStatesInTheBackground(t *testing.T) {
	f := newMergeFixture(t, "main")
	// Stored without planning, as on a server just started: nothing read yet.
	stored := func(status domain.TicketStatus) string {
		tk, err := sqlite.NewTicketRepository(f.db).Create(f.projectID, "task", "", status)
		if err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	a, b := stored(domain.TicketStatusReview), stored(domain.TicketStatusDone)
	f.session(a, "feat-a", "main", 1)
	f.session(b, "feat-b", "main", 1)
	f.merge("feat-b")

	n := f.feed.len()
	list, err := f.plan.ListTickets(f.ctx, f.projectID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	for _, tk := range list {
		if tk.MergeState != nil {
			t.Fatalf("%s merge state = %+v, want null until computed", tk.ID, tk.MergeState)
		}
	}
	f.announced(n, a, domain.MergeState{State: domain.MergeNotMerged, Merged: 0, Total: 1})
	f.announced(n, b, domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}
