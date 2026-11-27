package gateruns

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"operators-mcp/internal/domain"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Skipf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

func TestADiffIsFoundAndAnythingTheHarnessCannotReadIsTreatedAsADiff(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	base := gitIn(t, dir, "rev-parse", "HEAD")
	base = base[:len(base)-1]
	// Where git itself cannot run (a sandbox with no cache for the git shim), the
	// cases below would all read as unreadable: skip rather than pass for the wrong reason.
	gitIn(t, dir, "status", "--porcelain")
	gitIn(t, dir, "rev-list", "--count", base+"..HEAD")
	sess := &domain.Session{ID: "s", WorkingDir: dir, WorkspaceID: "w"}
	hasDiff := GitHasDiff(func(context.Context, string) string { return base })
	ctx := context.Background()

	if hasDiff(ctx, sess) {
		t.Error("a clean worktree at its base has a diff")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasDiff(ctx, sess) {
		t.Error("an uncommitted file is no diff")
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "work")
	if !hasDiff(ctx, sess) {
		t.Error("a commit since the base is no diff")
	}

	// Fail closed: what cannot be read counts as a diff, so the session is held to the Guard.
	if !hasDiff(ctx, &domain.Session{ID: "x", WorkingDir: filepath.Join(dir, "nope")}) {
		t.Error("a worktree that cannot be read counted as no diff")
	}
	if !GitHasDiff(func(context.Context, string) string { return "" })(ctx, &domain.Session{ID: "y", WorkingDir: t.TempDir()}) {
		t.Error("a directory that is no repository counted as no diff")
	}
	unknownBase := GitHasDiff(func(context.Context, string) string { return "" })
	if !unknownBase(ctx, sess) {
		t.Error("an unknown base on a clean tree counted as no diff")
	}
}

// fakeGit puts a git on PATH that answers what the test says, so the cases do
// not depend on a git that runs where the tests do.
func fakeGit(t *testing.T, status string, statusFails bool, count string, countFails bool) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n status) "
	if statusFails {
		script += "exit 128 ;;\n"
	} else {
		script += "printf '%s' '" + status + "' ;;\n"
	}
	script += " rev-list) "
	if countFails {
		script += "exit 128 ;;\n"
	} else {
		script += "echo " + count + " ;;\nesac\n"
	}
	if countFails {
		script += "esac\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestADiffIsReadFromGitAndWhatItCannotAnswerCountsAsADiff(t *testing.T) {
	sess := &domain.Session{ID: "s", WorkingDir: t.TempDir(), WorkspaceID: "w"}
	ctx := context.Background()
	withBase := GitHasDiff(func(context.Context, string) string { return "abc" })
	noBase := GitHasDiff(func(context.Context, string) string { return "" })

	cases := []struct {
		name        string
		status      string
		statusFails bool
		count       string
		countFails  bool
		has         func(context.Context, *domain.Session) bool
		want        bool
	}{
		{"clean at its base", "", false, "0", false, withBase, false},
		{"an uncommitted change", " M a.go", false, "0", false, withBase, true},
		{"a commit since the base", "", false, "3", false, withBase, true},
		{"git cannot read the status", "", true, "0", false, withBase, true},
		{"git cannot count the commits", "", false, "", true, withBase, true},
		{"a clean tree with no known base", "", false, "0", false, noBase, true},
		{"an uncommitted change needs no base", " M a.go", false, "0", false, noBase, true},
	}
	for _, c := range cases {
		fakeGit(t, c.status, c.statusFails, c.count, c.countFails)
		if got := c.has(ctx, sess); got != c.want {
			t.Errorf("%s: has a diff = %v, want %v", c.name, got, c.want)
		}
	}
}
