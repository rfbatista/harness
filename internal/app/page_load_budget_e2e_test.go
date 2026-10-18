//go:build !windows

package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/usage"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra/querycount"
	"operators-mcp/internal/ports"
)

// The page-load budget: a project the harness's own size (30 tasks, 150
// sessions with events, workspaces, usage and pull requests) on a SQLite
// file, read through the real services and the real web handler. The task
// page, the project page, the reviews page, the repositories page and a
// session's history page make as many database queries at 150 sessions as at
// 50, at 60 tasks as at 30 and at 4 repositories as at 1, which is the hard
// guarantee: their database work grows with none of them. Each answers
// within 50 ms when idle, and 10 of each loaded at once each within 250 ms.
// The race detector slows everything several times over, so the timings are
// only held without it.
func TestBudgetedPagesStayWithinThePageLoadBudget(t *testing.T) {
	b := newBudgetProject(t)
	pages := []struct{ name, path string }{
		{"task page", "/projects/" + b.project + "/tasks/" + b.tickets[0]},
		{"project page", "/projects/" + b.project},
		{"reviews page", "/projects/" + b.project + "/reviews"},
		{"repositories page", "/projects/" + b.project + "/repositories"},
		{"session history page", "/projects/" + b.project + "/tasks/" + b.tickets[0] + "/sessions/s-000/history"},
	}

	counts := func() []int64 {
		n := make([]int64, len(pages))
		for i, p := range pages {
			n[i] = b.queries(p.path)
		}
		return n
	}
	b.addSessions(50)
	at50 := counts()
	b.addSessions(100)
	at150 := counts()
	b.addTickets(30)
	at60Tasks := counts()
	b.addRepositories(3)
	at4Repos := counts()
	for i, p := range pages {
		t.Logf("%s: %d queries at 50 sessions, %d at 150, %d at 60 tasks, %d at 4 repositories", p.name, at50[i], at150[i], at60Tasks[i], at4Repos[i])
		if at50[i] != at150[i] {
			t.Errorf("the %s made %d queries at 50 sessions and %d at 150: its database work grows with the sessions", p.name, at50[i], at150[i])
		}
		if at150[i] != at60Tasks[i] {
			t.Errorf("the %s made %d queries at 30 tasks and %d at 60: its database work grows with the tasks", p.name, at150[i], at60Tasks[i])
		}
		if at60Tasks[i] != at4Repos[i] {
			t.Errorf("the %s made %d queries at 1 repository and %d at 4: its database work grows with the repositories", p.name, at60Tasks[i], at4Repos[i])
		}
	}

	for _, p := range pages {
		idle, parallel := b.idle(p.path), b.parallel(p.path, 10)
		t.Logf("%s: idle %v; 10 in parallel, slowest %v", p.name, idle, parallel)
		if raceEnabled {
			continue
		}
		if idle > 50*time.Millisecond {
			t.Errorf("the %s took %v when idle, over the 50 ms budget", p.name, idle)
		}
		if parallel > 250*time.Millisecond {
			t.Errorf("the slowest of 10 parallel loads of the %s took %v, over the 250 ms budget", p.name, parallel)
		}
	}
}

// budgetProject is the seeded project and the web handler that serves it.
type budgetProject struct {
	t        *testing.T
	ctx      context.Context
	handler  http.Handler
	db       *gorm.DB
	sessions ports.SessionRepository
	cat      catalog.Catalog
	plan     *planning.Service
	project  string
	repo     string
	repoDir  string
	tickets  []string
	seeded   int
}

func newBudgetProject(t *testing.T) *budgetProject {
	t.Helper()
	// Under Xcode, /usr/bin/git is a shim that looks the real git up on every
	// run, which costs up to 50 ms a run on some machines: that is the
	// machine's wait, not the server's, so the pages run the real git.
	if out, err := exec.Command("git", "--exec-path").Output(); err == nil {
		t.Setenv("PATH", strings.TrimSpace(string(out))+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)

	b := &budgetProject{t: t, ctx: context.Background()}
	var (
		cat     catalog.Catalog
		orch    *orchestration.Service
		plan    *planning.Service
		ws      *workspaces.Service
		prs     *workspaces.PullRequests
		code    ports.CodeBrowser
		art     *artifacts.Service
		usageSv *usage.Service
		inbox   optionalNotifications
		creds   optionalGitCredentials
	)
	// Without ExecutionModule: it registers Genkit flows in a process-wide
	// registry, so it cannot boot twice in one test binary.
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, GitCredentialsModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, UsageModule, NotificationsModule,
		fx.Populate(&cat, &orch, &plan, &ws, &prs, &code, &art, &usageSv, &inbox, &creds, &b.db, &b.sessions),
		fx.NopLogger,
	)
	if err := app.Start(b.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	p, err := cat.Projects.CreateProject(b.ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(b.ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	b.project, b.repo, b.repoDir = p.ID, r.ID, repoDir
	b.cat, b.plan = cat, plan
	b.addTickets(30)

	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	var notes ports.NotificationReader
	if inbox.Service != nil {
		notes = inbox.Service
	}
	b.handler = web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects,
		RunCommands: cat.Projects, Documents: plan, History: ws, Code: code, Workspaces: ws, Artifacts: art, ProjectSummaries: cat.Projects,
		PullRequests: prs, Resources: plan, Comments: plan, Notifications: notes, Usage: usageSv, TokenSavers: cat.Capabilities,
		TokenSaverInstalls: cat.Capabilities, GitCredentials: creds.port(),
	}, assets, nil)
	return b
}

// addRepositories adds n more repositories to the project, each a checkout
// of its own.
func (b *budgetProject) addRepositories(n int) {
	b.t.Helper()
	for i := range n {
		dir := filepath.Join(b.t.TempDir(), "repo")
		gitInit(b.t, dir)
		if _, err := b.cat.Projects.CreateRepository(b.ctx, b.project, fmt.Sprintf("repo-%d", i), "", fmt.Sprintf("https://example.com/repo-%d.git", i), dir); err != nil {
			b.t.Fatal(err)
		}
	}
}

// addTickets creates n more tasks, each with a pending review.
func (b *budgetProject) addTickets(n int) {
	b.t.Helper()
	reviews := sqlite.NewReviewRequestRepository(b.db)
	for range n {
		tk, err := b.plan.CreateTicket(b.ctx, b.project, fmt.Sprintf("Task %02d", len(b.tickets)), "A task of the budget project.", domain.TicketStatusInProgress)
		if err != nil {
			b.t.Fatal(err)
		}
		b.tickets = append(b.tickets, tk.ID)
		if _, err := reviews.Create(&domain.ReviewRequest{TaskID: tk.ID, ProjectID: b.project, Subject: "Review the plan", Body: "Look at it.", State: domain.ReviewPending}); err != nil {
			b.t.Fatal(err)
		}
	}
}

// addSessions seeds n more sessions, spread over the tasks in turn. Each has
// a workspace, a few stored events and a usage record; every other one has a
// pull request; the first on a task is its architect, the others its
// delegates, each with a status check.
func (b *budgetProject) addSessions(n int) {
	b.t.Helper()
	workspacesRepo := sqlite.NewWorkspaceRepository(b.db)
	usageRepo := sqlite.NewUsageRepository(b.db)
	pullRequests := sqlite.NewPullRequestRepository(b.db)
	checks := sqlite.NewStatusCheckRepository(b.db)
	for range n {
		i := b.seeded
		b.seeded++
		ticket := b.tickets[i%len(b.tickets)]
		branch, path := fmt.Sprintf("agent/s-%d", i), b.t.TempDir()
		if i == 0 {
			b.worktree(branch, path) // the session whose history page is read
		}
		w, err := workspacesRepo.Create(domain.Workspace{RepositoryID: b.repo, Name: fmt.Sprintf("ws-%d", i), Branch: branch, Path: path, BaseRef: "main"})
		if err != nil {
			b.t.Fatal(err)
		}
		mode, status := domain.SessionModeImplement, domain.SessionDone
		if i < len(b.tickets) {
			mode = domain.SessionModeArchitect
		}
		if i%3 == 0 {
			status = domain.SessionRunning
		}
		s, err := b.sessions.Create(&domain.Session{
			ID: fmt.Sprintf("s-%03d", i), ProjectID: b.project, RepositoryID: b.repo, TicketID: ticket, Task: "Work on the task",
			WorkingDir: w.Path, Status: status, WorkspaceID: w.ID, Branch: w.Branch, Interactive: i%2 == 0, RunsOn: domain.RunnerServer, Mode: mode,
		})
		if err != nil {
			b.t.Fatal(err)
		}
		for seq := range int64(5) {
			if err := b.sessions.RecordEvent(s.ID, ports.SessionEventRecord{Seq: seq + 1, Type: "output", Payload: []byte(`{"type":"output","text":"working"}`)}); err != nil {
				b.t.Fatal(err)
			}
		}
		if err := usageRepo.Create(b.ctx, &domain.UsageRecord{Attribution: s.ID, ProjectID: b.project, TaskID: ticket, SessionID: s.ID, Model: "claude-sonnet-4-5",
			Status: 200, StartedAt: time.Now(), InputTokens: 1000, OutputTokens: 200}); err != nil {
			b.t.Fatal(err)
		}
		if i%2 == 0 {
			if _, err := pullRequests.Save(&domain.PullRequest{WorkspaceID: w.ID, RepositoryID: b.repo, ProjectID: b.project, Number: i + 1,
				URL: fmt.Sprintf("https://example.com/pull/%d", i+1), Title: "Work", HeadBranch: w.Branch, BaseBranch: "main", State: domain.PullRequestOpen}); err != nil {
				b.t.Fatal(err)
			}
		}
		if mode == domain.SessionModeImplement {
			if err := checks.Save(&domain.StatusCheck{TaskID: ticket, ArchitectSessionID: fmt.Sprintf("s-%03d", i%len(b.tickets)), DelegateSessionID: s.ID,
				EveryMinutes: 10, State: domain.StatusCheckActive, ProjectID: b.project}); err != nil {
				b.t.Fatal(err)
			}
		}
	}
}

// worktree checks a new branch out at path, with a commit of its own and an
// uncommitted change, as a session's worktree would be.
func (b *budgetProject) worktree(branch, path string) {
	b.t.Helper()
	for _, c := range []struct {
		dir  string
		args []string
	}{
		{b.repoDir, []string{"worktree", "add", "-q", "-b", branch, path}},
		{path, []string{"-c", "user.email=e2e@test", "-c", "user.name=e2e", "commit", "-q", "--allow-empty", "-m", "work"}},
	} {
		cmd := exec.Command("git", c.args...)
		cmd.Dir = c.dir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.t.Fatalf("git %v: %v\n%s", c.args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "notes.txt"), []byte("work in progress\n"), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

// load serves one GET of path on the calling goroutine and answers how long
// it took and how many database queries it made.
func (b *budgetProject) load(path string) (time.Duration, int64) {
	b.t.Helper()
	ctx, counter, stop := querycount.Track(b.ctx)
	defer stop()
	rec := httptest.NewRecorder()
	started := time.Now()
	b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	took := time.Since(started)
	if rec.Code != http.StatusOK {
		b.t.Fatalf("GET %s: %d\n%s", path, rec.Code, rec.Body.String())
	}
	return took, counter.Count()
}

func (b *budgetProject) queries(path string) int64 {
	_, n := b.load(path)
	return n
}

// idle is the best of a few loads of path, one at a time: the first warms
// the connections and caches up.
func (b *budgetProject) idle(path string) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 5 {
		took, _ := b.load(path)
		best = min(best, took)
	}
	return best
}

// parallel loads path n times at once, a few rounds, and answers the slowest
// load of the best round.
func (b *budgetProject) parallel(path string, n int) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 3 {
		took := make([]time.Duration, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				took[i], _ = b.load(path)
			}()
		}
		wg.Wait()
		best = min(best, slices.Max(took))
	}
	return best
}
