package workspaces_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
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
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	merges := workspaces.NewMergeStates(workspaces.MergeStateDeps{
		Sessions: sqlite.NewSessionRepository(db), Workspaces: sqlite.NewWorkspaceRepository(db),
		Repositories: repos, Git: gitcli.NewBranchMerges(),
	})
	plan.Decorator = merges
	return &mergeFixture{t: t, ctx: context.Background(), db: db, plan: plan, merges: merges, projectID: p.ID, repoID: repo.ID, repoDir: dir, worktrees: map[string]string{}}
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

// gitOut runs git in dir and answers its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// forgetReflog drops the branch's reflog, as git does when it expires.
func (f *mergeFixture) forgetReflog(branch string) {
	f.t.Helper()
	gitRun(f.t, f.repoDir, "reflog", "expire", "--expire=now", "refs/heads/"+branch)
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
// from base, with commits commits of its own. Its workspace records no base
// commit, as those made before the harness stored one.
func (f *mergeFixture) session(taskID, branch, base string, commits int) {
	f.t.Helper()
	f.sessionAt(taskID, branch, base, commits, false)
}

// sessionAt is session; withBaseSHA records the commit the branch was cut at
// on its workspace, as the harness does for new workspaces.
func (f *mergeFixture) sessionAt(taskID, branch, base string, commits int, withBaseSHA bool) {
	f.t.Helper()
	path := filepath.Join(f.t.TempDir(), branch)
	gitRun(f.t, f.repoDir, "worktree", "add", "-b", branch, path, base)
	baseSHA := ""
	if withBaseSHA {
		baseSHA = gitOut(f.t, f.repoDir, "rev-parse", "refs/heads/"+branch)
	}
	f.worktrees[branch] = path
	for i := range commits {
		name := filepath.Join(path, branch+".txt")
		if err := os.WriteFile(name, []byte{byte('a' + i)}, 0o644); err != nil {
			f.t.Fatal(err)
		}
		gitRun(f.t, path, "add", ".")
		gitRun(f.t, path, "commit", "-m", branch)
	}
	ws, err := sqlite.NewWorkspaceRepository(f.db).Create(domain.Workspace{RepositoryID: f.repoID, Name: branch, Branch: branch, Path: path, BaseRef: base, BaseSHA: baseSHA})
	if err != nil {
		f.t.Fatal(err)
	}
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

// A merged branch has nothing ahead of its base by name; the commit its
// workspace was cut at still tells it carries work of its own, whatever its
// reflog kept.
func TestAMergedBranchCountsByTheCommitItsWorkspaceWasCutAt(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.sessionAt(id, "feat", "main", 1, true)
	f.merge("feat")
	f.forgetReflog("feat")
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

// A workspace the harness makes records the commit its branch was cut at, so
// the branch still counts once merged and its reflog is gone.
func TestAWorkspaceTheHarnessMakesKeepsTheCommitItWasCutAt(t *testing.T) {
	f := newMergeFixture(t, "main")
	settings := sqlite.NewSettingsRepository(f.db)
	if err := settings.Set(domain.SettingWorkspacesRoot, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	svc := workspaces.NewService(sqlite.NewWorkspaceRepository(f.db), sqlite.NewRepositoryRepository(f.db), gitcli.NewWorktreeManager(), settings)
	cut := gitOut(t, f.repoDir, "rev-parse", "refs/heads/main")
	ws, err := svc.Create(f.repoID, "feat", "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if ws.BaseSHA != cut {
		t.Fatalf("base sha = %q, want %q", ws.BaseSHA, cut)
	}
	if err := os.WriteFile(filepath.Join(ws.Path, "feat.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, ws.Path, "add", ".")
	gitRun(t, ws.Path, "commit", "-m", "feat")
	id := f.task(domain.TicketStatusReview)
	if _, err := sqlite.NewSessionRepository(f.db).Create(&domain.Session{
		ProjectID: f.projectID, TicketID: id, RepositoryID: f.repoID, WorkspaceID: ws.ID, Branch: "feat",
		Status: domain.SessionDone, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	f.merge("feat")
	f.forgetReflog("feat")
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeMerged, Merged: 1, Total: 1})
}

// A workspace from before the base commit was stored, whose reflog expired,
// falls back to the commits ahead of its base: once merged there are none.
func TestAnOldWorkspaceWithoutReflogFallsBackToItsBaseByName(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	f.session(id, "feat", "main", 1)
	f.merge("feat")
	f.forgetReflog("feat")
	wantMerge(t, f.read(id), domain.MergeState{State: domain.MergeNone})
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
