package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/ports"
)

// A service started on a branch runs in its own service worktree, detached at
// the branch's tip (ADR 0080).

func branchRunFixture(t *testing.T) (*servicesFixture, *gitRepo, string) {
	t.Helper()
	f, repo := gitFixture(t, services.Options{
		WorktreeRoot: t.TempDir(),
		EnvWriter:    gitcli.NewEnvFiles(gitcli.WithGit(realGit)),
	})
	repo.git("checkout", "-q", "-b", "feature")
	repo.commit("feature.txt", "feature v1\n")
	repo.commit("tracked.txt", "tracked v1\n")
	repo.git("checkout", "-q", "main")
	in := commandInput("web")
	in["repository_id"] = f.repoID
	in["export_repository_env"] = true
	in["command"] = map[string]any{"line": "cat feature.txt; ls -a; sleep 30"}
	return f, repo, f.createID(in)
}

func (f *servicesFixture) startOn(id, worktree string) (int, map[string]any) {
	f.t.Helper()
	return f.call("POST", "/api/start_service", map[string]any{"service_id": id, "worktree": worktree})
}

func refsOf(repo *gitRepo) string {
	return repo.git("for-each-ref", "--format=%(refname) %(objectname)")
}

func TestServicesBranchRun_ALocalBranchCheckedOutElsewhereRunsAtItsTipInTheServiceWorktree(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	repo.git("worktree", "add", "-q", elsewhere, "feature")
	t.Cleanup(func() { f.operate("stop_service", id) })
	tip := repo.git("rev-parse", "feature")
	before := refsOf(repo)

	code, out := f.startOn(id, "branch:feature")
	if code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	run := f.awaitStatus(id, "running")["run"].(map[string]any)
	where := run["where"].(map[string]any)
	if where["kind"] != "branch" || where["branch"] != "feature" || where["sha"] != tip[:7] || where["remote"] != false ||
		!strings.HasPrefix(where["dir"].(string), f.worktreeRoot) || strings.Contains(where["dir"].(string), repo.dir) || run["worktree_key"] != "branch:feature" {
		t.Fatalf("where = %v, run = %v", where, run)
	}
	if text := f.awaitLog(id, "feature v1"); !strings.Contains(text, "feature @ "+tip[:7]) {
		t.Fatalf("the log names the branch and its short commit: %s", text)
	}
	if after := refsOf(repo); after != before {
		t.Fatalf("a branch ref was created or moved:\n%s\n--\n%s", before, after)
	}
	if cur := repo.git("symbolic-ref", "--short", "HEAD"); cur != "main" {
		t.Fatalf("the main checkout moved to %s", cur)
	}
	if _, err := os.Stat(filepath.Join(repo.dir, "feature.txt")); err == nil {
		t.Fatal("the branch's files reached the main checkout")
	}
}

func TestServicesBranchRun_EachStartRunsTheLatestTipResetsTrackedFilesAndKeepsIgnoredOnes(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	t.Cleanup(func() { f.operate("stop_service", id) })
	repo.git("checkout", "-q", "feature")
	repo.commit(".gitignore", "node_modules/\n")
	repo.git("checkout", "-q", "main")
	f.startOn(id, "branch:feature")
	dir := f.awaitStatus(id, "running")["run"].(map[string]any)["where"].(map[string]any)["dir"].(string)
	f.operate("stop_service", id)
	f.awaitStatus(id, "stopped")

	// Work inside the service worktree: an ignored dependency, an untracked
	// file, and a tracked file edited.
	for path, content := range map[string]string{"node_modules/dep/x.js": "dep", "scratch.txt": "mine", "tracked.txt": "edited"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The branch moves on.
	repo.git("checkout", "-q", "feature")
	repo.commit("feature.txt", "feature v2\n")
	repo.git("checkout", "-q", "main")

	f.startOn(id, "branch:feature")
	f.awaitLog(id, "feature v2")
	for path, want := range map[string]string{"node_modules/dep/x.js": "dep", "scratch.txt": "mine", "tracked.txt": "tracked v1\n"} {
		got, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestServicesBranchRun_TheSavedEnvFilesGoIntoTheServiceWorktreeAndNeverTheMainCheckout(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	t.Cleanup(func() { f.operate("stop_service", id) })
	if _, err := f.cat.Projects.SaveEnvFile(context.Background(), f.repoID, ".env", "TOKEN=abc\n"); err != nil {
		t.Fatal(err)
	}
	f.startOn(id, "branch:feature")
	dir := f.awaitStatus(id, "running")["run"].(map[string]any)["where"].(map[string]any)["dir"].(string)
	if got, err := os.ReadFile(filepath.Join(dir, ".env")); err != nil || string(got) != "TOKEN=abc\n" {
		t.Fatalf("service worktree .env = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(repo.dir, ".env")); err == nil {
		t.Fatal("an env file was written into the main checkout")
	}
}

func TestServicesBranchRun_RestartRunsTheSameBranchAtItsLatestTipAndAStartBeginsInTheMainCheckout(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	t.Cleanup(func() { f.operate("stop_service", id) })
	f.startOn(id, "branch:feature")
	f.awaitLog(id, "feature v1")

	repo.git("checkout", "-q", "feature")
	newTip := repo.commit("feature.txt", "feature v3\n")
	repo.git("checkout", "-q", "main")
	if code, out := f.operate("restart_service", id); code != 200 {
		t.Fatalf("restart = %d %v", code, out)
	}
	run := f.awaitStatus(id, "running")["run"].(map[string]any)
	if w := run["where"].(map[string]any); w["kind"] != "branch" || w["sha"] != newTip[:7] || run["worktree_key"] != "branch:feature" {
		t.Fatalf("restart = %v", run)
	}
	f.awaitLog(id, "feature v3")

	f.operate("stop_service", id)
	f.operate("start_service", id)
	if w := f.awaitStatus(id, "running")["run"].(map[string]any)["where"].(map[string]any); w["kind"] != "main" || w["dir"] != repo.dir {
		t.Fatalf("a start with no worktree runs in the main checkout: %v", w)
	}
}

func TestServicesBranchRun_AnUnknownBranchOrAServiceWithNothingToPickIsRefusedAndStartsNothing(t *testing.T) {
	f, _, id := branchRunFixture(t)
	code, out := f.startOn(id, "branch:nope")
	c, msg := errorOf(out)
	if code != 409 || c != "WORKTREE_NOT_AVAILABLE" || msg != `branch "nope" does not exist in api` {
		t.Fatalf("unknown branch = %d %v", code, out)
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("a refused start starts nothing: %s", st)
	}
	for _, other := range []string{f.createID(imageInput("db")), f.createID(commandInput("loose"))} {
		code, out := f.startOn(other, "branch:feature")
		if c, _ := errorOf(out); code != 400 || c != "INVALID_INPUT" {
			t.Fatalf("branch on a service with nothing to pick = %d %v", code, out)
		}
	}
}

// failingCheckout is the real git with a checkout that fails.
type failingCheckout struct{ ports.ServiceBranchGit }

func (failingCheckout) CheckoutDetached(context.Context, string, string) error {
	return errors.New("git checkout: error: unable to write the tree")
}

func TestServicesBranchRun_ACheckoutFailureEndsTheRunFailedWithGitsReason(t *testing.T) {
	f, _, id := branchRunFixture(t)
	g := restarted(t, f, services.Options{
		WorktreeRoot: f.worktreeRoot,
		BranchGit:    failingCheckout{gitcli.NewServiceBranches(gitcli.WithGit(realGit))},
	})
	g.startOn(id, "branch:feature")
	svc := g.awaitStatus(id, "failed")
	if run := svc["run"].(map[string]any); !strings.Contains(run["error"].(string), "unable to write the tree") {
		t.Fatalf("run = %v", run)
	}
}

func TestServicesBranchRun_AMissingSubfolderAtTheBranchFailsTheRunNamingTheFolderAndTheWorktree(t *testing.T) {
	f, repo, _ := branchRunFixture(t)
	repo.commit("cmd/main.go", "package main\n") // on main only: feature has no cmd
	in := commandInput("sub")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": "sleep 30", "subdir": "cmd"}
	id := f.createID(in)

	f.startOn(id, "branch:feature")
	run := f.awaitStatus(id, "failed")["run"].(map[string]any)
	dir := run["where"].(map[string]any)["dir"].(string)
	base := strings.TrimSuffix(dir, "/cmd")
	if want := `subfolder "cmd" does not exist in ` + base; run["error"] != want {
		t.Fatalf("error = %q, want %q", run["error"], want)
	}
}
