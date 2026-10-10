package portstest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// CodeFixture is a real repository the code suite reads, built with git:
//
//	main:    root commit (files, a binary, a 1 MiB+ file, a file with no
//	         trailing newline, symlinks inside and outside the root, a
//	         .gitignore, bulk/ files whose lines pass the diff total), then a
//	         rename with an edit, then a merge of the branch side.
//	session: a worktree cut from main (BaseRef "main") with a committed
//	         change and addition, and on disk an edit, a deletion, an
//	         untracked file and an ignored one.
//	gone:    a branch whose worktree was removed from disk.
type CodeFixture struct {
	// RepoDir is the main checkout, on main.
	RepoDir string
	// Worktree is the session worktree; GoneWorktree a path no longer on
	// disk.
	Worktree, GoneWorktree string
	// Branch and GoneBranch are the two workspaces' branches; BaseRef what
	// both were cut from.
	Branch, GoneBranch, BaseRef string

	// Commits on main, newest last, and the tips of the other branches.
	Root, Rename, Merge, SideTip, SessionHead, GoneHead string
	// EmptyTree is the empty tree's hash, a root commit's base.
	EmptyTree string

	// ProjectID is the project the zones belong to; the runner records the
	// repository under it.
	ProjectID string
	// Zones and Contexts are the project's architecture: "Adapters"
	// (pattern ^internal/) in the context "Core", and "Web" (explicit path
	// web) in none.
	Zones    []*domain.Zone
	Contexts []*domain.BoundedContext
}

// Architecture is the fixture's zones and contexts as the read port serves
// them.
func (f *CodeFixture) Architecture() StaticArchitecture {
	return StaticArchitecture{ProjectID: f.ProjectID, Zones: f.Zones, Contexts: f.Contexts}
}

// StaticArchitecture is a ports.ArchitectureMap over fixed zones and
// contexts of one project.
type StaticArchitecture struct {
	ProjectID string
	Zones     []*domain.Zone
	Contexts  []*domain.BoundedContext
}

func (a StaticArchitecture) ListZones(projectID string) []*domain.Zone {
	if projectID != a.ProjectID {
		return nil
	}
	return a.Zones
}

func (a StaticArchitecture) ListBoundedContexts(projectID string) []*domain.BoundedContext {
	if projectID != a.ProjectID {
		return nil
	}
	return a.Contexts
}

// Fixture sizes.
const (
	fixtureBulkFiles = 7    // × fixtureBulkLines lines: past domain.MaxDiffLines together
	fixtureBulkLines = 2990 // each under domain.MaxDiffFileLines
	fixtureBigLines  = 40_000
)

// NewCodeFixture builds the fixture repository under t.TempDir().
func NewCodeFixture(t *testing.T) *CodeFixture {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	f := &CodeFixture{
		RepoDir: repo, Worktree: filepath.Join(base, "wt-session"), GoneWorktree: filepath.Join(base, "wt-gone"),
		Branch: "session", GoneBranch: "gone", BaseRef: "main", ProjectID: "code-project",
		Zones: []*domain.Zone{
			{ID: "zone-adapters", ProjectID: "code-project", Name: "Adapters", Pattern: "^internal/", BoundedContextID: "ctx-core"},
			{ID: "zone-web", ProjectID: "code-project", Name: "Web", ExplicitPaths: []string{"web"}},
		},
		Contexts: []*domain.BoundedContext{{ID: "ctx-core", ProjectID: "code-project", Name: "Core", Purpose: "the core"}},
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.com",
			"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
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
	symlink := func(dir, target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	git(repo, "init", "-q", "-b", "main")
	write(repo, "README.md", "hello\n")
	write(repo, ".gitignore", "ignored.log\n")
	write(repo, "internal/app/app.go", "package app\n\nfunc Run() {}\n")
	write(repo, "web/src/main.js", "console.log('a')\n")
	write(repo, "docs/old.md", "# Guide\n\none\ntwo\nthree\nfour\nfive\nsix\n")
	write(repo, "nonl.txt", "first\nlast")
	write(repo, "logo.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00binary")
	var big strings.Builder
	for i := range fixtureBigLines {
		fmt.Fprintf(&big, "line %06d of the big file\n", i)
	}
	write(repo, "big.txt", big.String())
	for i := range fixtureBulkFiles {
		var b strings.Builder
		for j := range fixtureBulkLines {
			fmt.Fprintf(&b, "bulk %d line %d\n", i, j)
		}
		write(repo, fmt.Sprintf("bulk/%c.txt", 'a'+i), b.String())
	}
	symlink(repo, "README.md", "link-in")
	symlink(repo, "../../outside", "link-out")
	symlink(repo, "internal", "linkdir")
	git(repo, "add", "-A")
	git(repo, "commit", "-q", "-m", "root")
	f.Root = git(repo, "rev-parse", "HEAD")

	git(repo, "branch", "side")
	git(repo, "mv", "docs/old.md", "docs/new.md")
	write(repo, "docs/new.md", "# Guide\n\none\ntwo\nthree\nfour\nfive\nsix\nseven\n")
	write(repo, "internal/app/app.go", "package app\n\n// Run runs.\nfunc Run() {}\n")
	git(repo, "add", "-A")
	git(repo, "commit", "-q", "-m", "rename the guide")
	f.Rename = git(repo, "rev-parse", "HEAD")

	git(repo, "checkout", "-q", "side")
	write(repo, "side.txt", "from the side\n")
	git(repo, "add", "-A")
	git(repo, "commit", "-q", "-m", "side")
	f.SideTip = git(repo, "rev-parse", "HEAD")
	git(repo, "checkout", "-q", "main")
	git(repo, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	f.Merge = git(repo, "rev-parse", "HEAD")

	git(repo, "worktree", "add", "-q", "-b", f.Branch, f.Worktree, "main")
	wt := f.Worktree
	write(wt, "web/src/main.js", "console.log('b')\n")
	write(wt, "internal/app/feature.go", "package app\n\nfunc Feature() {}\n")
	git(wt, "add", "-A")
	git(wt, "commit", "-q", "-m", "session work")
	f.SessionHead = git(wt, "rev-parse", "HEAD")
	write(wt, "README.md", "hello\nworld\n")
	if err := os.Remove(filepath.Join(wt, "nonl.txt")); err != nil {
		t.Fatal(err)
	}
	write(wt, "new.txt", "brand\nnew\n")
	write(wt, "ignored.log", "noise\n")

	git(repo, "worktree", "add", "-q", "-b", f.GoneBranch, f.GoneWorktree, "main")
	write(f.GoneWorktree, "gone.txt", "committed before the worktree went\n")
	git(f.GoneWorktree, "add", "-A")
	git(f.GoneWorktree, "commit", "-q", "-m", "gone work")
	f.GoneHead = git(f.GoneWorktree, "rev-parse", "HEAD")
	if err := os.RemoveAll(f.GoneWorktree); err != nil {
		t.Fatal(err)
	}

	f.EmptyTree = git(repo, "hash-object", "-t", "tree", "/dev/null")
	return f
}
