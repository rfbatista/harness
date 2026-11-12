package httpapi

import (
	"os"
	"strings"
	"testing"
)

// The service worktree goes when its service, its repository or its project
// does (ADR 0080).

// ranOnBranch starts the service on the branch and stops it, and answers the
// service worktree's directory.
func ranOnBranch(t *testing.T, f *servicesFixture, id string) string {
	t.Helper()
	if code, out := f.startOn(id, "branch:feature"); code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	dir := f.awaitStatus(id, "running")["run"].(map[string]any)["where"].(map[string]any)["dir"].(string)
	f.operate("stop_service", id)
	f.awaitStatus(id, "stopped")
	return dir
}

func gone(t *testing.T, repo *gitRepo, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the service worktree %s is still on disk: %v", dir, err)
	}
	if list := repo.git("worktree", "list", "--porcelain"); strings.Contains(list, dir) {
		t.Fatalf("git still lists %s:\n%s", dir, list)
	}
}

func TestServicesBranchCleanup_DeletingTheServiceRemovesItsServiceWorktree(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	dir := ranOnBranch(t, f, id)
	listed := repo.git("worktree", "list", "--porcelain")
	if !strings.Contains(listed, dir) {
		t.Fatalf("git lists the service worktree while it exists:\n%s", listed)
	}

	if code, out := f.call("POST", "/api/delete_service", map[string]any{"service_id": id}); code != 200 {
		t.Fatalf("delete = %d %v", code, out)
	}
	gone(t, repo, dir)
	if _, err := os.Stat(repo.dir + "/.git"); err != nil {
		t.Fatalf("the main checkout is touched: %v", err)
	}
}

func TestServicesBranchCleanup_ADeleteSucceedsWhenThereIsNoServiceWorktreeOrItWasDeletedByHand(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	if code, out := f.call("POST", "/api/delete_service", map[string]any{"service_id": id}); code != 200 {
		t.Fatalf("a service that never ran on a branch = %d %v", code, out)
	}
	id2 := f.command("second", "true")
	in := commandInput("third")
	in["repository_id"] = f.repoID
	in["command"] = map[string]any{"line": "sleep 30"}
	id3 := f.createID(in)
	f.startOn(id3, "branch:feature")
	dir := f.awaitStatus(id3, "running")["run"].(map[string]any)["where"].(map[string]any)["dir"].(string)
	f.operate("stop_service", id3)
	f.awaitStatus(id3, "stopped")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{id2, id3} {
		if code, out := f.call("POST", "/api/delete_service", map[string]any{"service_id": id}); code != 200 {
			t.Fatalf("delete %s = %d %v", id, code, out)
		}
	}
	repo.git("worktree", "prune")
}

func TestServicesBranchCleanup_RemovingTheRepositoryRemovesTheServiceWorktreesAndStartIsRefusedAfterIt(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	f.svc.Subscribe(f.cat.Bus)
	dir := ranOnBranch(t, f, id)

	if code, out := f.call("POST", "/api/delete_repository", map[string]any{"repository_id": f.repoID}); code != 204 {
		t.Fatalf("delete_repository = %d %v", code, out)
	}
	gone(t, repo, dir)
	if _, err := f.store.GetService(id); err != nil {
		t.Fatalf("the service stays so the person can edit or delete it: %v", err)
	}
	for _, key := range []string{"branch:feature", "main", ""} {
		code, out := f.startOn(id, key)
		if c, _ := errorOf(out); c != "REPOSITORY_NOT_FOUND" || code == 200 {
			t.Fatalf("start on %q = %d %v, want REPOSITORY_NOT_FOUND", key, code, out)
		}
	}
	if code, out := f.operate("restart_service", id); code == 200 {
		t.Fatalf("restart of a service whose repository is gone = %d %v", code, out)
	} else if c, _ := errorOf(out); c != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("restart = %d %v", code, out)
	}
	if st := f.status(id); st != "stopped" {
		t.Fatalf("a refused start starts nothing: %s", st)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("a refused start made the service worktree again")
	}
}

func TestServicesBranchCleanup_DeletingTheProjectRemovesEveryServiceWorktreeEvenAfterTheCheckoutIsGone(t *testing.T) {
	f, repo, id := branchRunFixture(t)
	f.svc.Subscribe(f.cat.Bus)
	dir := ranOnBranch(t, f, id)
	// The repository's checkout is deleted first: git can no longer be asked
	// through it.
	if err := os.RemoveAll(repo.dir); err != nil {
		t.Fatal(err)
	}

	if code, out := f.call("POST", "/api/delete_project", map[string]any{"project_id": f.projectID}); code != 204 {
		t.Fatalf("delete_project = %d %v", code, out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the service worktree %s is still on disk: %v", dir, err)
	}
	if _, err := f.store.GetService(id); err == nil {
		t.Fatal("the service is still stored")
	}
}
