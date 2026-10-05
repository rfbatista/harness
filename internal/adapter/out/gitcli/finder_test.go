package gitcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if remote != "" {
		if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", remote).CombinedOutput(); err != nil {
			t.Fatalf("git remote: %v %s", err, out)
		}
	}
}

func TestFindRepositoriesInsideAProject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	gitInit(t, filepath.Join(root, "api"), "git@github.com:me/api.git")
	gitInit(t, filepath.Join(root, "web"), "")
	gitInit(t, filepath.Join(root, "libs", "kit"), "https://example.com/kit.git")
	gitInit(t, filepath.Join(root, "api", "nested"), "")                    // inside a checkout: not searched
	gitInit(t, filepath.Join(root, ".cache", "hidden"), "")                 // hidden: skipped
	gitInit(t, filepath.Join(root, "node_modules", "dep"), "")              // skipped
	gitInit(t, filepath.Join(root, "a", "b", "c", "too-deep"), "")          // deeper than depth
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil { // not a checkout
		t.Fatal(err)
	}

	found, err := NewRepositoryFinder().FindRepositories(root, 3)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range found {
		rel, _ := filepath.Rel(root, r.Path)
		got = append(got, rel+" "+r.Name+" "+r.Remote)
	}
	want := []string{
		"api api git@github.com:me/api.git",
		"libs/kit kit https://example.com/kit.git",
		"web web ",
	}
	if len(got) != len(want) {
		t.Fatalf("found %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("found[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFindRepositoriesWhenTheRootIsOne(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	gitInit(t, root, "")
	gitInit(t, filepath.Join(root, "sub"), "")
	found, err := NewRepositoryFinder().FindRepositories(root, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Path != filepath.Clean(root) {
		t.Fatalf("found %+v, want just the root", found)
	}
}

func TestFindRepositoriesInAMissingDirectoryFails(t *testing.T) {
	if _, err := NewRepositoryFinder().FindRepositories(filepath.Join(t.TempDir(), "nope"), 3); err == nil {
		t.Fatal("want an error")
	}
}
