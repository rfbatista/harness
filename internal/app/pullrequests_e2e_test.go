package app

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/ghcli/ghclitest"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// prServer is the wired server stack on a database file, with real git and
// only the code host faked.
type prServer struct {
	app  *fx.App
	cat  catalog.Catalog
	orch *orchestration.Service
	plan *planning.Service
	ws   *workspaces.Service
	prs  *workspaces.PullRequests
	sess ports.SessionRepository
	e    *archE2E
}

func startPRServer(t *testing.T, dir, dbPath string, host *ghclitest.FakeHost) *prServer {
	t.Helper()
	s := &prServer{}
	s.app = fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: dbPath, Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		WithCodeHost(host),
		fx.Populate(&s.cat, &s.orch, &s.plan, &s.ws, &s.prs, &s.sess),
		fx.NopLogger,
	)
	if err := s.app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *prServer) serve(t *testing.T, projectID string) *archE2E {
	t.Helper()
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Sessions: s.orch, PullRequests: s.prs})))
	t.Cleanup(srv.Close)
	e := &archE2E{t: t, ctx: context.Background(), srv: srv, project: projectID}
	e.follow()
	return e
}

func gitE2E(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=e2e@test", "-c", "user.name=e2e"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A session's branch opened as a pull request from the API: the draft, the
// push to origin, the PR stored and announced, a second create refused with
// it, the record surviving a restart, and a merge on the host reaching the
// feed after a read.
func TestPullRequests_EndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "harness.db")
	widgets := domain.GitRemote{Host: "github.com", Owner: "acme", Repo: "widgets"}

	// The checkout's origin reads as GitHub; git's url.insteadOf lands every
	// fetch and push in a local bare repository.
	repoDir := filepath.Join(dir, "widgets")
	gitInit(t, repoDir)
	bare := filepath.Join(dir, "origin.git")
	gitE2E(t, dir, "init", "-q", "--bare", bare)
	gitE2E(t, repoDir, "remote", "add", "origin", "git@github.com:acme/widgets.git")
	gitE2E(t, repoDir, "config", "url."+bare+".insteadOf", "git@github.com:acme/widgets.git")
	gitE2E(t, repoDir, "push", "-q", "origin", "main")
	gitE2E(t, repoDir, "fetch", "-q", "origin")

	host := ghclitest.New()
	s := startPRServer(t, dir, dbPath, host)
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = s.app.Stop(context.Background())
		}
	})
	if _, err := s.cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := s.cat.Projects.CreateProject(ctx, "widgets", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.cat.Projects.CreateRepository(ctx, p.ID, "widgets", "", "git@github.com:acme/widgets.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.plan.CreateTicket(ctx, p.ID, "Create pull request", "Open a PR from the UI", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.ws.Create(r.ID, "pr-e2e", "agent/pr-e2e", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.sess.Create(&domain.Session{ID: "s-pr", ProjectID: p.ID, RepositoryID: r.ID, TicketID: tk.ID, Task: "pr",
		WorkingDir: ws.Path, WorkspaceID: ws.ID, Branch: ws.Branch, Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}
	e := s.serve(t, p.ID)
	get := "/api/pull_request?workspace_id=" + ws.ID

	// Nothing committed yet: the draft says why create would refuse.
	status, out := e.http("GET", get, "")
	draft, _ := out["draft"].(map[string]any)
	if status != 200 || draft["title"] != "Create pull request" || draft["base_branch"] != "main" || out["pull_request"] != nil {
		t.Fatalf("GET = %d %v", status, out)
	}
	if b := draft["blockers"].([]any); len(b) != 1 || b[0] != "PR_NO_COMMITS" {
		t.Fatalf("blockers = %v", b)
	}

	// The session commits one change and leaves another uncommitted.
	if err := os.WriteFile(filepath.Join(ws.Path, "feature.txt"), []byte("f\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, ws.Path, "add", ".")
	gitE2E(t, ws.Path, "commit", "-q", "-m", "Add the feature")
	if err := os.WriteFile(filepath.Join(ws.Path, "wip.txt"), []byte("w\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out = e.http("GET", get, "")
	draft = out["draft"].(map[string]any)
	if draft["ahead"] != float64(1) || draft["uncommitted_changes"] != float64(1) || len(draft["blockers"].([]any)) != 0 ||
		!strings.Contains(draft["body"].(string), "Open a PR from the UI\n\n- Add the feature") {
		t.Fatalf("draft after a commit = %v", draft)
	}

	status, out = e.http("POST", "/api/create_pull_request", `{"workspace_id":"`+ws.ID+`","title":" Create pull request ","body":"Why"}`)
	if status != 200 {
		t.Fatalf("create = %d %v", status, out)
	}
	pr := out["pull_request"].(map[string]any)
	if pr["number"] != float64(1) || pr["head_branch"] != "agent/pr-e2e" || pr["base_branch"] != "main" || pr["state"] != "open" || pr["title"] != "Create pull request" {
		t.Fatalf("created = %v", pr)
	}
	if head, pushed := gitE2E(t, ws.Path, "rev-parse", "HEAD"), gitE2E(t, bare, "rev-parse", "refs/heads/agent/pr-e2e"); head != pushed {
		t.Fatalf("origin has %s, the branch is at %s", pushed, head)
	}
	if got := gitE2E(t, repoDir, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Fatalf("the checkout moved to %s", got)
	}
	e.sawOnFeed("pull_request", `"number":1`, `"workspace_id":"`+ws.ID+`"`, `"state":"open"`)
	if opened := host.Opened(); len(opened) != 1 || opened[0].Title != "Create pull request" || opened[0].Body != "Why" || opened[0].Base != "main" || opened[0].Head != "agent/pr-e2e" {
		t.Fatalf("opened on the host = %+v", opened)
	}

	status, out = e.http("POST", "/api/create_pull_request", `{"workspace_id":"`+ws.ID+`","title":"Again"}`)
	details, _ := out["details"].(map[string]any)
	if status != 409 || out["code"] != "PR_ALREADY_EXISTS" || details["pull_request"].(map[string]any)["number"] != float64(1) {
		t.Fatalf("second create = %d %v", status, out)
	}

	// The server restarts; meanwhile the PR is merged on GitHub.
	if err := s.app.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopped = true
	host.SetState(widgets, 1, domain.PullRequestMerged)
	s = startPRServer(t, dir, dbPath, host)
	t.Cleanup(func() { _ = s.app.Stop(context.Background()) })
	e = s.serve(t, p.ID)

	_, out = e.http("GET", get, "")
	if pr := out["pull_request"].(map[string]any); pr["number"] != float64(1) || pr["state"] != "open" {
		t.Fatalf("GET after restart = %v; it answers the record as stored", pr)
	}
	e.sawOnFeed("pull_request", `"number":1`, `"state":"merged"`)
	_, out = e.http("GET", get, "")
	if pr := out["pull_request"].(map[string]any); pr["state"] != "merged" {
		t.Fatalf("second GET = %v, want merged", pr)
	}
	if b := out["draft"].(map[string]any)["blockers"].([]any); len(b) != 0 {
		t.Fatalf("blockers after the merge = %v; a merged PR blocks nothing", b)
	}

	// gh signed out on the server machine.
	host.SignOut()
	_, out = e.http("GET", get, "")
	if b := out["draft"].(map[string]any)["blockers"].([]any); len(b) != 1 || b[0] != "PR_TOOL_UNAVAILABLE" {
		t.Fatalf("blockers signed out = %v", b)
	}
	status, out = e.http("POST", "/api/create_pull_request", `{"workspace_id":"`+ws.ID+`","title":"Follow-up"}`)
	if status != 503 || out["code"] != "PR_TOOL_UNAVAILABLE" || !strings.Contains(out["error"].(string), "gh auth login") {
		t.Fatalf("create signed out = %d %v", status, out)
	}
	host.SignIn()

	// A follow-up PR closed on the host reaches the feed at once through
	// RefreshNow, with no wait for the 30 s throttle.
	if status, out = e.http("POST", "/api/create_pull_request", `{"workspace_id":"`+ws.ID+`","title":"Follow-up"}`); status != 200 {
		t.Fatalf("follow-up = %d %v", status, out)
	}
	number := int(out["pull_request"].(map[string]any)["number"].(float64))
	host.SetState(widgets, number, domain.PullRequestClosed)
	if err := s.prs.RefreshNow(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	e.sawOnFeed("pull_request", `"number":`+strconv.Itoa(number), `"state":"closed"`)
	stored, err := s.prs.StoredPullRequests(ctx, []string{ws.ID})
	if err != nil || len(stored) != 1 || stored[0].Number != number || stored[0].State != domain.PullRequestClosed {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}
