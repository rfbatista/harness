package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/ports"
)

// A service started on a branch of origin fetches that branch first (ADR 0080).

// noCredentialAuth stands for a repository whose origin needs none: a file
// path, or a host the tests never reach.
type noCredentialAuth struct{ ports.GitAuth }

func (noCredentialAuth) Resolve(context.Context, string, string) (*ports.GitResolution, error) {
	return &ports.GitResolution{}, nil
}
func (noCredentialAuth) Operation(context.Context, string, string) (*ports.GitOperation, error) {
	return &ports.GitOperation{Release: func() {}}, nil
}
func (noCredentialAuth) Used(string) {}

func remoteFixture(t *testing.T) (f *servicesFixture, repo *gitRepo, other *gitRepo, id string) {
	t.Helper()
	publisher := gitcli.NewBranchPublisher(gitcli.WithGit(realGit))
	fetch := workspaces.NewServiceBranchFetch(publisher, workspaces.NewOriginFetcher(publisher, noCredentialAuth{}))
	f, repo = gitFixture(t, services.Options{WorktreeRoot: t.TempDir(), Fetcher: fetch})
	bare := repo.withOrigin()
	// A second clone of origin pushes the branch's new commits.
	clone := t.TempDir()
	other = &gitRepo{t: t, dir: clone, at: repo.at}
	other.git("clone", "-q", bare.dir, clone+"/c")
	other.dir = clone + "/c"
	other.git("checkout", "-q", "-b", "feature/x")
	other.commit("x.txt", "x v1\n")
	other.git("push", "-q", "origin", "feature/x")
	repo.git("fetch", "-q", "origin")

	in := commandInput("web")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": "cat x.txt; sleep 30"}
	id = f.createID(in)
	t.Cleanup(func() { f.operate("stop_service", id) })
	return f, repo, other, id
}

func TestServicesBranchRemote_AStartFetchesTheBranchFirstAndARestartFetchesAgain(t *testing.T) {
	f, repo, other, id := remoteFixture(t)
	repo.git("fetch", "-q", "origin") // the tracking ref holds v1
	other.commit("x.txt", "x v2\n")
	other.git("push", "-q", "origin", "feature/x")
	tip2 := other.git("rev-parse", "HEAD")

	if code, out := f.startOn(id, "branch:origin/feature/x"); code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	run := f.awaitStatus(id, "running")["run"].(map[string]any)
	where := run["where"].(map[string]any)
	if where["kind"] != "branch" || where["branch"] != "feature/x" || where["remote"] != true || where["sha"] != tip2[:7] || run["worktree_key"] != "branch:origin/feature/x" {
		t.Fatalf("where = %v, run = %v", where, run)
	}
	f.awaitLog(id, "x v2")

	other.commit("x.txt", "x v3\n")
	other.git("push", "-q", "origin", "feature/x")
	if code, out := f.operate("restart_service", id); code != 200 {
		t.Fatalf("restart = %d %v", code, out)
	}
	f.awaitStatus(id, "running")
	f.awaitLog(id, "x v3")
}

func TestServicesBranchRemote_ABranchGoneFromOriginFailsTheRunAndLeavesTheServiceWorktreeAsItWas(t *testing.T) {
	f, repo, other, id := remoteFixture(t)
	f.startOn(id, "branch:origin/feature/x")
	run := f.awaitStatus(id, "running")["run"].(map[string]any)
	dir := run["where"].(map[string]any)["dir"].(string)
	f.operate("stop_service", id)
	f.awaitStatus(id, "stopped")
	before := (&gitRepo{t: t, dir: dir}).git("rev-parse", "HEAD")

	other.git("push", "-q", "origin", ":feature/x") // still listed here: nobody fetched --prune
	_ = repo
	if code, out := f.startOn(id, "branch:origin/feature/x"); code != 200 {
		t.Fatalf("a branch that is still listed is accepted: %d %v", code, out)
	}
	failed := f.awaitStatus(id, "failed")["run"].(map[string]any)
	if msg, _ := failed["error"].(string); !strings.Contains(msg, "feature/x") {
		t.Fatalf("run = %v", failed)
	}
	if after := (&gitRepo{t: t, dir: dir}).git("rev-parse", "HEAD"); after != before {
		t.Fatalf("the service worktree moved: %s -> %s", before, after)
	}
}

func TestServicesBranchRemote_AFailedFetchNamesNoCredential(t *testing.T) {
	f, repo, _, id := remoteFixture(t)
	const secret = "s3cretTOKEN-value"
	repo.git("remote", "set-url", "origin", "https://deploy:"+secret+"@127.0.0.1:1/org/repo.git")

	f.startOn(id, "branch:origin/feature/x")
	failed := f.awaitStatus(id, "failed")["run"].(map[string]any)
	if failed["error"] == "" {
		t.Fatalf("a fetch that cannot reach origin fails the run: %v", failed)
	}
	if strings.Contains(failed["error"].(string), secret) || strings.Contains(f.logText(id), secret) {
		t.Fatalf("a credential reached the error or the log: %q / %s", failed["error"], f.logText(id))
	}
	if _, err := os.Stat(filepath.Join(f.worktreeRoot, id)); err == nil {
		t.Fatal("the service worktree was made before the fetch succeeded")
	}
}
