package gitcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func fixtureRepo(t *testing.T, gitignore string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitOut(t, repo, "init", "-q")
	if gitignore != "" {
		if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(gitignore), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func mkHome(t *testing.T, parent string) string {
	t.Helper()
	home := filepath.Join(parent, "claude-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestTheRepositoryGitignoreIgnoresTheAgentHome(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "..", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	repo := fixtureRepo(t, string(content))
	home := mkHome(t, repo)
	if err := exec.Command("git", "-C", repo, "check-ignore", "-q", home).Run(); err != nil {
		t.Errorf("git does not ignore %s with the repository .gitignore: %v", home, err)
	}
	if HomeExposedToGit("", home) {
		t.Error("an ignored home reported as exposed")
	}
}

func TestAHomeInAWorkTreeWhereGitDoesNotIgnoreItIsExposed(t *testing.T) {
	repo := fixtureRepo(t, "")
	if !HomeExposedToGit("", mkHome(t, repo)) {
		t.Error("an unignored home in a work tree not reported")
	}
}

func TestAHomeOutsideAnyWorkTreeIsNotExposed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if HomeExposedToGit("", mkHome(t, t.TempDir())) {
		t.Error("a home outside any work tree reported as exposed")
	}
}

func TestAMissingHomeOrGitIsNotReported(t *testing.T) {
	if HomeExposedToGit("", filepath.Join(t.TempDir(), "nope")) {
		t.Error("a missing home reported as exposed")
	}
	repo := fixtureRepo(t, "")
	if HomeExposedToGit("/nonexistent/git", mkHome(t, repo)) {
		t.Error("a failing git reported as exposed")
	}
}
