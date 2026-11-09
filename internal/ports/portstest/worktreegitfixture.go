package portstest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// WorktreeGitFixture is a repository whose origin is a bare repository on
// disk, with one session worktree per upstream state. Its remote is a file
// path, so no credential resolves or is needed.
//
//	main:    one commit, pushed to origin.
//	nopush:  a worktree whose branch was never pushed (no upstream).
//	insync, ahead, behind, diverged, deleted: each pushed with its upstream set;
//	         then ahead has a local commit (and, on disk, a staged edit, an
//	         unstaged edit and an untracked file), behind a commit pushed to
//	         origin by another clone, diverged both, and deleted lost its
//	         branch on origin.
//	gone:    a branch whose worktree was removed from disk.
//
// The repository was never fetched: until Fetch runs, every upstream that
// exists is unknown. The remote-tracking refs git keeps from the pushes say
// nothing of what origin has since.
type WorktreeGitFixture struct {
	// RepoDir is the main checkout, on main; OriginDir the bare origin.
	RepoDir, OriginDir string
	// MirrorDir is a second bare remote, "mirror", of the main checkout: a push
	// to a named remote other than origin lands there.
	MirrorDir string
	// NoOriginDir is a second repository with no origin remote at all.
	NoOriginDir string
	// Worktrees are the session worktrees by branch name.
	Worktrees map[string]string
	// BaseRef is what every workspace was cut from.
	BaseRef string
	// Heads are the worktrees' HEADs by branch name.
	Heads map[string]string

	other string // another clone of origin, which pushes behind the repository's back
	git   func(dir string, args ...string) string
}

// WorktreeGitBranches are the fixture's session branches, in the order a task
// lists them.
var WorktreeGitBranches = []string{"nopush", "insync", "ahead", "behind", "diverged", "deleted", "gone"}

// NewWorktreeGitFixture builds the fixture under t.TempDir().
func NewWorktreeGitFixture(t *testing.T) *WorktreeGitFixture {
	t.Helper()
	base := t.TempDir()
	f := &WorktreeGitFixture{
		NoOriginDir: filepath.Join(base, "no-origin"), RepoDir: filepath.Join(base, "repo"), OriginDir: filepath.Join(base, "origin.git"), MirrorDir: filepath.Join(base, "mirror.git"), other: filepath.Join(base, "other"),
		Worktrees: map[string]string{}, Heads: map[string]string{}, BaseRef: "main",
	}
	f.git = func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.com",
			"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-c", "protocol.file.allow=always"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(dir, name, content string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(dir, name, content string) {
		write(dir, name, content)
		f.git(dir, "add", name)
		f.git(dir, "commit", "-q", "-m", "add "+name)
	}

	for _, d := range []string{f.RepoDir, f.OriginDir, f.MirrorDir, f.NoOriginDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.git(f.NoOriginDir, "init", "-q", "-b", "main")
	write(f.NoOriginDir, "README.md", "alone\n")
	f.git(f.NoOriginDir, "add", "-A")
	f.git(f.NoOriginDir, "commit", "-q", "-m", "root")
	f.git(f.OriginDir, "init", "-q", "--bare", "-b", "main")
	f.git(f.RepoDir, "init", "-q", "-b", "main")
	write(f.RepoDir, "README.md", "hello\n")
	write(f.RepoDir, "tracked.txt", "one\ntwo\n")
	write(f.RepoDir, "staged.txt", "one\n")
	f.git(f.RepoDir, "add", "-A")
	f.git(f.RepoDir, "commit", "-q", "-m", "root")
	f.git(f.MirrorDir, "init", "-q", "--bare", "-b", "main")
	f.git(f.RepoDir, "remote", "add", "origin", f.OriginDir)
	f.git(f.RepoDir, "remote", "add", "mirror", f.MirrorDir)
	f.git(f.RepoDir, "push", "-q", "-u", "origin", "main")

	for _, name := range WorktreeGitBranches {
		wt := filepath.Join(base, "wt-"+name)
		f.git(f.RepoDir, "worktree", "add", "-q", "-b", name, wt, "main")
		f.Worktrees[name] = wt
		if name != "nopush" {
			f.git(wt, "push", "-q", "-u", "origin", name)
		}
	}
	// ahead, diverged: local commits; the ahead worktree is also dirty.
	commit(f.Worktrees["ahead"], "ahead.txt", "local\n")
	commit(f.Worktrees["diverged"], "diverged-local.txt", "local\n")
	commit(f.Worktrees["nopush"], "nopush.txt", "local\n")
	wt := f.Worktrees["ahead"]
	write(wt, "tracked.txt", "one\ntwo\nthree\n") // edited, not staged
	write(wt, "staged.txt", "one\nstaged\n")
	f.git(wt, "add", "staged.txt") // edited and staged
	write(wt, "untracked.txt", "new\n")

	// origin moves behind the repository's back: behind and diverged get a
	// commit there, and deleted loses its branch.
	f.git(base, "clone", "-q", f.OriginDir, f.other)
	for _, name := range []string{"behind", "diverged"} {
		f.git(f.other, "checkout", "-q", "-B", name, "origin/"+name)
		commit(f.other, "remote-"+name+".txt", "remote\n")
		f.git(f.other, "push", "-q", "origin", name)
	}
	f.git(f.OriginDir, "branch", "-q", "-D", "deleted")

	// gone: committed work, then the worktree goes.
	commit(f.Worktrees["gone"], "gone.txt", "committed before the worktree went\n")
	f.git(f.Worktrees["gone"], "push", "-q", "origin", "gone")
	f.git(f.RepoDir, "worktree", "remove", "--force", f.Worktrees["gone"])
	for name, dir := range f.Worktrees {
		if name != "gone" {
			f.Heads[name] = f.git(dir, "rev-parse", "HEAD")
		}
	}
	f.Heads["gone"] = f.git(f.RepoDir, "rev-parse", "refs/heads/gone")
	return f
}

// Fetch runs `git fetch --prune origin` in the main checkout, as a person
// would, so the remote-tracking refs and the fetch time follow origin.
func (f *WorktreeGitFixture) Fetch(t *testing.T) {
	t.Helper()
	f.git(f.RepoDir, "fetch", "-q", "--prune", "origin")
}

// Git runs git in dir for a test that moves the fixture on.
func (f *WorktreeGitFixture) Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return f.git(dir, args...)
}

// PushFromOtherClone makes origin's branch move: another clone adds file to it
// and pushes, so the repository's remote-tracking ref is stale until a fetch.
func (f *WorktreeGitFixture) PushFromOtherClone(t *testing.T, branch, file string) {
	t.Helper()
	f.git(f.other, "fetch", "-q", "origin")
	f.git(f.other, "checkout", "-q", "-B", branch, "origin/"+branch)
	if err := os.WriteFile(filepath.Join(f.other, file), []byte("remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git(f.other, "add", file)
	f.git(f.other, "commit", "-q", "-m", "remote "+file)
	f.git(f.other, "push", "-q", "origin", branch)
}

// OriginRev is the commit origin holds for branch ("" when it has none).
func (f *WorktreeGitFixture) OriginRev(t *testing.T, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = f.OriginDir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// RefuseInOrigin installs a pre-receive hook in origin that declines pushes to
// branch, so a push of it is refused for what it carries; the returned func
// removes it.
func (f *WorktreeGitFixture) RefuseInOrigin(t *testing.T, branch string) (remove func()) {
	t.Helper()
	hook := filepath.Join(f.OriginDir, "hooks", "pre-receive")
	script := "#!/bin/sh\nwhile read old new ref; do\n  if [ \"$ref\" = \"refs/heads/" + branch + "\" ]; then echo \"declined by the fixture's hook\" >&2; exit 1; fi\ndone\nexit 0\n"
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Remove(hook) }
}

// MirrorRev is the commit the mirror remote holds for branch ("" when none).
func (f *WorktreeGitFixture) MirrorRev(t *testing.T, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = f.MirrorDir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
