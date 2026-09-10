package gitcli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/application/ports"
)

// initRepo creates a throwaway git repository with one commit on branch "main".
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{
			"-c", "user.name=test", "-c", "user.email=test@test",
			"-c", "init.defaultBranch=main",
		}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "init")
	return dir
}

// gitIn runs git in dir and fails the test on error. Identity flags are set so
// the test does not depend on the developer's global git config.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-c", "user.name=test", "-c", "user.email=test@test",
	}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestWorktreeManager_AddNewBranch(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreeManager()
	path := filepath.Join(t.TempDir(), "ws", "feature-x")

	if err := w.Add(repo, path, "feature-x", ""); err != nil {
		t.Fatal(err)
	}
	// A linked worktree has a .git *file* pointing at the main repository.
	if fi, err := os.Stat(filepath.Join(path, ".git")); err != nil || fi.IsDir() {
		t.Fatalf(".git file missing in worktree: fi=%v err=%v", fi, err)
	}
}

func TestWorktreeManager_AddFromBaseBranch(t *testing.T) {
	repo := initRepo(t)
	// A commit that exists only on "base", so its presence in the worktree
	// proves the branch was cut from base rather than from HEAD.
	gitIn(t, repo, "checkout", "-b", "base")
	if err := os.WriteFile(filepath.Join(repo, "BASE.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-m", "base only")
	gitIn(t, repo, "checkout", "main")

	w := NewWorktreeManager()
	path := filepath.Join(t.TempDir(), "ws", "feature-y")
	if err := w.Add(repo, path, "feature-y", "base"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "BASE.md")); err != nil {
		t.Fatalf("worktree was not cut from base: %v", err)
	}
}

func TestWorktreeManager_AddFromRemoteRef(t *testing.T) {
	repo := initRepo(t)
	// A remote-tracking ref without a remote: update-ref is enough for git to
	// treat origin/main as a valid start point.
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")

	w := NewWorktreeManager()
	path := filepath.Join(t.TempDir(), "ws", "feature-z")
	if err := w.Add(repo, path, "feature-z", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Fatalf("worktree missing base content: %v", err)
	}
}

func TestWorktreeManager_AddExistingBranchFails(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "branch", "spare")

	w := NewWorktreeManager()
	path := filepath.Join(t.TempDir(), "ws", "spare")
	err := w.Add(repo, path, "spare", "main")
	if !errors.Is(err, ports.ErrBranchExists) {
		t.Fatalf("Add on an existing branch = %v, want ports.ErrBranchExists", err)
	}
}

func TestWorktreeManager_Remove(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreeManager()
	path := filepath.Join(t.TempDir(), "ws", "gone")

	if err := w.Add(repo, path, "gone", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.Remove(repo, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree directory should be gone, stat err=%v", err)
	}
}

func TestWorktreeManager_RemoveToleratesOutOfBandDeletedDirectory(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreeManager()
	path := filepath.Join(t.TempDir(), "ws", "vanished")

	if err := w.Add(repo, path, "vanished", ""); err != nil {
		t.Fatal(err)
	}

	// Simulate out-of-band deletion: user `rm -rf`, disk cleanup, etc. The
	// worktree directory (and its parent) disappears without git ever being
	// told. Removing the parent too (not just the leaf) is what actually
	// makes `git worktree remove --force` fail on a modern git: it errors
	// with "is not a working tree" when the leaf's containing directory is
	// also gone, whereas a missing leaf alone in an otherwise-intact parent
	// is silently tolerated.
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}

	if err := w.Remove(repo, path); err != nil {
		t.Fatalf("Remove should tolerate an already-gone worktree, got: %v", err)
	}

	// The stale registration must be gone too: adding a new worktree with a
	// different branch at the same path should succeed cleanly.
	if err := w.Add(repo, path, "vanished-again", ""); err != nil {
		t.Fatalf("Add at the same path should succeed after prune, got: %v", err)
	}
}

// TestWorktreeManager_AddRejectsOptionLikeBaseRef proves the "--" separator
// stops git from reinterpreting an attacker- or typo-supplied baseRef as an
// option: a baseRef of "--lock" must fail as an invalid ref, never silently
// lock the worktree (which `worktree remove --force` then refuses to undo).
func TestWorktreeManager_AddRejectsOptionLikeBaseRef(t *testing.T) {
	repo := initRepo(t)
	w := NewWorktreeManager()
	path := filepath.Join(t.TempDir(), "ws", "injected")

	err := w.Add(repo, path, "injected", "--lock")
	if err == nil {
		t.Fatal("expected an error for an option-shaped baseRef")
	}
	if strings.Contains(err.Error(), "worktree remove") {
		t.Fatalf("baseRef must not be interpreted as an option: %v", err)
	}
	// The worktree must not have been created at all, locked or otherwise.
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("worktree should not exist after a rejected baseRef, stat err=%v", statErr)
	}
}

func TestWorktreeManager_DeleteBranch(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "branch", "spare")

	w := NewWorktreeManager()
	if err := w.DeleteBranch(repo, "spare"); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/spare")
	cmd.Dir = repo
	if err := cmd.Run(); err == nil {
		t.Fatal("branch should be gone after DeleteBranch")
	}
}

func TestWorktreeManager_AddErrorIncludesStderr(t *testing.T) {
	w := NewWorktreeManager()
	err := w.Add(t.TempDir(), filepath.Join(t.TempDir(), "x"), "b", "")
	if err == nil {
		t.Fatal("expected error adding worktree in a non-git directory")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Fatalf("error should mention git: %v", err)
	}
}

func TestWorktreeManager_ListBranches(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "branch", "spare")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	// origin/HEAD is a symbolic alias, not a branch anyone should base work on.
	gitIn(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	branches, err := NewWorktreeManager().ListBranches(repo)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, b := range branches {
		names = append(names, b.Name)
	}
	want := []string{"main", "spare", "origin/main"}
	if !slices.Equal(names, want) {
		t.Fatalf("branches = %v, want %v", names, want)
	}
	if !branches[0].IsHead || branches[0].Remote {
		t.Fatalf("main should be the local HEAD branch: %+v", branches[0])
	}
	if !branches[2].Remote || branches[2].IsHead {
		t.Fatalf("origin/main should be a non-HEAD remote branch: %+v", branches[2])
	}
}
