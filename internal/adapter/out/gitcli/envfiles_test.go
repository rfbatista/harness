package gitcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

// A repository with one commit and a worktree cut from it, as sessions get.
func repoWithWorktree(t *testing.T) (repo, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo = filepath.Join(t.TempDir(), "repo")
	gitInit(t, repo, "")
	gitOut(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "init")
	worktree = filepath.Join(t.TempDir(), "wt")
	gitOut(t, repo, "worktree", "add", "-q", "-b", "session", worktree)
	return repo, worktree
}

func TestEnvFilesWritesIntoTheWorktreeAndKeepsThemOutOfGit(t *testing.T) {
	repo, wt := repoWithWorktree(t)
	files := []*domain.EnvFile{
		{Path: ".env", Content: "DATABASE_URL=postgres://local\n"},
		{Path: "apps/api/.env.local", Content: "API_KEY=secret\n"},
	}
	if err := NewEnvFiles().Write(wt, files); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		p := filepath.Join(wt, filepath.FromSlash(f.Path))
		b, err := os.ReadFile(p)
		if err != nil || string(b) != f.Content {
			t.Fatalf("%s: %q %v", f.Path, b, err)
		}
		if info, _ := os.Stat(p); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", f.Path, info.Mode().Perm())
		}
	}
	if status := gitOut(t, wt, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
		t.Fatalf("git sees the env files: %q", status)
	}

	// Writing again (the next session) does not repeat the exclude lines.
	if err := NewEnvFiles().Write(wt, files); err != nil {
		t.Fatal(err)
	}
	exclude, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(exclude), "/.env\n") != 1 || strings.Count(string(exclude), "/apps/api/.env.local\n") != 1 {
		t.Fatalf("info/exclude = %q", exclude)
	}
}

func TestEnvFilesLeavesAlreadyIgnoredPathsAlone(t *testing.T) {
	repo, wt := repoWithWorktree(t)
	if err := os.WriteFile(filepath.Join(wt, ".gitignore"), []byte(".env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewEnvFiles().Write(wt, []*domain.EnvFile{{Path: ".env", Content: "A=1"}}); err != nil {
		t.Fatal(err)
	}
	exclude, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Contains(string(exclude), "/.env") {
		t.Fatalf("an ignored path was added to info/exclude: %q", exclude)
	}
}

func TestEnvFilesReadFromTheCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := NewEnvFiles().Read(root, ".env"); err != nil || string(b) != "A=1\n" {
		t.Fatalf("read: %q %v", b, err)
	}
	if _, err := NewEnvFiles().Read(root, "missing/.env"); errs.Code(err) != "ENV_FILE_NOT_FOUND" {
		t.Fatalf("missing: %v", err)
	}
}
