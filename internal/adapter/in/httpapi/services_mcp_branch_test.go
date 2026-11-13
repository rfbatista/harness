package httpapi

import (
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/application/workspaces"
)

// An agent starts or restarts a service on a branch through an approved
// service request (ADR 0086; contract "Harness server ↔ Agent sessions — the
// services MCP server", revision 5).

// branchMCPFixture is the services MCP server over a real repository with a
// "feature" branch and a command service "web" that prints the branch's file.
func branchMCPFixture(t *testing.T) (f *mcpFixture, repo *gitRepo, id string) {
	t.Helper()
	root := t.TempDir()
	publisher := gitcli.NewBranchPublisher(gitcli.WithGit(realGit))
	fetch := workspaces.NewServiceBranchFetch(publisher, workspaces.NewOriginFetcher(publisher, noCredentialAuth{}))
	f = newMCPFixtureBase(t, 5*time.Second, true, func(o *services.Options) {
		o.WorktreeRoot = root
		o.BranchGit = gitcli.NewServiceBranches(gitcli.WithGit(realGit))
		o.Fetcher = fetch
		o.EnvWriter = gitcli.NewEnvFiles(gitcli.WithGit(realGit))
	})
	repo = initGitRepo(t, f.repoDir())
	repo.git("checkout", "-q", "-b", "feature")
	repo.commit("feature.txt", "feature v1\n")
	repo.git("checkout", "-q", "main")
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	in := commandInput("web")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": "cat feature.txt; sleep 30"}
	id = f.createID(in)
	t.Cleanup(func() { f.operate("stop_service", id) })
	return f, repo, id
}

func TestServicesMCP_BranchStartFilesARequestWithTheBranchAndRunsAtItsTipOnApproval(t *testing.T) {
	f, repo, id := branchMCPFixture(t)
	tip := repo.git("rev-parse", "feature")[:7]
	agent := f.as("sess-1")

	done := agent.callAsync("start_service", map[string]any{"name": "web", "worktree": "branch:feature", "reason": "set the task up"})
	r := f.awaitPending(1)[0]
	f.stillWaiting(done)
	branch, _ := r["branch"].(map[string]any)
	if r["worktree"] != "branch:feature" || branch["name"] != "feature" || branch["sha"] != tip || branch["remote"] != false {
		t.Fatalf("request = %v", r)
	}
	if r["summary"] != "start web on branch feature @ "+tip {
		t.Fatalf("summary = %q", r["summary"])
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("nothing starts before the answer: %s", st)
	}

	// The person approves; the branch moves on in the meantime, and the run
	// takes place at the tip the approval finds.
	repo.git("checkout", "-q", "feature")
	newTip := repo.commit("feature.txt", "feature v2\n")[:7]
	repo.git("checkout", "-q", "main")
	f.answer(r["id"].(string), "approve", "")
	got := f.waitingFor(done)
	if got.msg != "" {
		t.Fatalf("start refused: %s", got.msg)
	}
	f.awaitStatus(id, "running")
	f.awaitLog(id, "feature v2")
	run := viewRunOf(f.as("sess-1").ok("service_status", named("web")))
	if run["where"] != "branch feature @ "+newTip || run["branch"] != "feature" || !strings.HasPrefix(run["dir"].(string), f.worktreeRoot) {
		t.Fatalf("run = %v", run)
	}
}

func TestServicesMCP_BranchOnOriginIsFetchedBeforeTheRequestIsFiled(t *testing.T) {
	f, repo, id := branchMCPFixture(t)
	bare := repo.withOrigin()
	clone := &gitRepo{t: t, dir: t.TempDir(), at: repo.at}
	clone.git("clone", "-q", bare.dir, clone.dir+"/c")
	clone.dir += "/c"
	clone.git("checkout", "-q", "-b", "only/remote")
	tip := clone.commit("feature.txt", "remote v1\n")[:7]
	clone.git("push", "-q", "origin", "only/remote")

	done := f.as("sess-1").callAsync("start_service", map[string]any{"name": "web", "worktree": "branch:origin/only/remote"})
	r := f.awaitPending(1)[0]
	branch, _ := r["branch"].(map[string]any)
	if branch["name"] != "only/remote" || branch["sha"] != tip || branch["remote"] != true || r["worktree"] != "branch:origin/only/remote" {
		t.Fatalf("request = %v", r)
	}
	f.answer(r["id"].(string), "approve", "")
	f.waitingFor(done)
	f.awaitLog(id, "remote v1")
}

func TestServicesMCP_BranchRefusalsCarryTheContractsMessagesAndFileNoRequest(t *testing.T) {
	f, _, _ := branchMCPFixture(t)
	f.createID(commandInput("job"))
	img := imageInput("db")
	img["repository_id"] = f.repoID
	f.createID(img)
	agent := f.as("sess-1")

	for _, tool := range []string{"start_service", "restart_service"} {
		agent.refused(tool, map[string]any{"name": "web", "worktree": "branch:nope"}, `branch "nope" does not exist in api`)
		agent.refused(tool, map[string]any{"name": "web", "worktree": "branch:origin/nope"}, `branch "nope" does not exist in api`)
		agent.refused(tool, map[string]any{"name": "job", "worktree": "branch:feature"}, `"job" cannot run on a branch: it has no repository`)
		agent.refused(tool, map[string]any{"name": "db", "worktree": "branch:feature"}, `"db" cannot run on a branch: it is an image service`)
		for _, bad := range []string{"main", "", "branch:", "origin/feature"} {
			agent.refused(tool, map[string]any{"name": "web", "worktree": bad}, `worktree must be "own", "branch:<name>" or omitted`)
		}
	}
	if n := len(f.pendingRequests()); n != 0 {
		t.Fatalf("a refusal files no request: %d pending", n)
	}
}

func TestServicesMCP_RestartRepeatsTheBranchAtItsLatestTipAndTakesAnotherBranchWhenAsked(t *testing.T) {
	f, repo, id := branchMCPFixture(t)
	f.autoApprove()
	agent := f.as("sess-1")
	agent.ok("start_service", map[string]any{"name": "web", "worktree": "branch:feature"})
	f.awaitLog(id, "feature v1")

	repo.git("checkout", "-q", "feature")
	newTip := repo.commit("feature.txt", "feature v2\n")[:7]
	repo.git("checkout", "-q", "main")
	run := agent.ok("restart_service", named("web"))["service"].(map[string]any)["run"].(map[string]any)
	if run["where"] != "branch feature @ "+newTip || run["branch"] != "feature" {
		t.Fatalf("a restart repeats the branch at its latest tip: %v", run)
	}
	f.awaitLog(id, "feature v2")

	repo.git("checkout", "-q", "-b", "other")
	repo.commit("feature.txt", "other v1\n")
	repo.git("checkout", "-q", "main")
	run = agent.ok("restart_service", map[string]any{"name": "web", "worktree": "branch:other"})["service"].(map[string]any)["run"].(map[string]any)
	if run["branch"] != "other" || !strings.HasPrefix(run["where"].(string), "branch other @ ") {
		t.Fatalf("restart on another branch = %v", run)
	}

	// Once the branch is gone a restart that repeats it is refused.
	repo.git("branch", "-D", "other")
	agent.refused("restart_service", named("web"), `branch "other" does not exist in api`)
}

func TestServicesMCP_RestartRepeatsAMainCheckoutRunAndAnOwnWorktreeRunAsBefore(t *testing.T) {
	f, repo, _ := branchMCPFixture(t)
	f.autoApprove()
	agent := f.as("sess-1")
	agent.ok("start_service", named("web"))
	repo.git("checkout", "-q", "main")
	if run := agent.ok("restart_service", named("web"))["service"].(map[string]any)["run"].(map[string]any); run["where"] != "main checkout" {
		t.Fatalf("restart after a main checkout run = %v", run)
	}
}
