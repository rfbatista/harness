package apps_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/domain"
)

// envFiles is the repository settings' saved env files.
type envFiles map[string][]*domain.EnvFile

func (e envFiles) ListEnvFiles(_ context.Context, repositoryID string) ([]*domain.EnvFile, error) {
	return e[repositoryID], nil
}

// newGitRunner is newRunner on a worktree git knows, writing env files the
// way session start does.
func newGitRunner(t *testing.T, saved envFiles) (*apps.Service, string) {
	t.Helper()
	svc, worktree := newRunner(t)
	if out, err := exec.Command("git", "init", "-q", worktree).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	svc.UseEnvFiles(saved, gitcli.NewEnvFiles())
	return svc, worktree
}

// An edit in settings reaches the next run of a session already running, and
// the saved config wins over a worktree copy changed in between.
func TestEachRunRewritesTheRepositorysEnvFilesIntoTheWorktree(t *testing.T) {
	saved := envFiles{"r1": {
		{RepositoryID: "r1", Path: ".env", Content: "GREETING=first\n"},
		{RepositoryID: "r1", Path: "web/.env.local", Content: "WEB=first\n"},
	}}
	svc, worktree := newGitRunner(t, saved)
	ctx := context.Background()
	show := `printf 'env:'; cat .env web/.env.local | tr '\n' ' '; echo :end; sleep 30`

	r, err := svc.Start(ctx, "s1", "", show)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, svc, r.ID, "env:GREETING=first WEB=first :end")
	_, _ = svc.Stop(ctx, r.ID)

	saved["r1"] = []*domain.EnvFile{
		{RepositoryID: "r1", Path: ".env", Content: "GREETING=second\n"},
		{RepositoryID: "r1", Path: "web/.env.local", Content: "WEB=second\n"},
	}
	if err := os.WriteFile(filepath.Join(worktree, ".env"), []byte("GREETING=agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err = svc.Start(ctx, "s1", "", show)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, svc, r.ID, "env:GREETING=second WEB=second :end")

	for _, rel := range []string{".env", "web/.env.local"} {
		fi, err := os.Stat(filepath.Join(worktree, rel))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", rel, fi.Mode().Perm())
		}
	}
	exclude, err := os.ReadFile(filepath.Join(worktree, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"/.env", "/web/.env.local"} {
		if strings.Count(string(exclude), line+"\n") != 1 {
			t.Errorf("info/exclude lists %s %d times, want once:\n%s", line, strings.Count(string(exclude), line+"\n"), exclude)
		}
	}
}

// A run that cannot get its env files is refused before anything starts, and
// the refusal never carries a value from them.
func TestARunWhoseEnvFilesCannotBeWrittenIsRefused(t *testing.T) {
	saved := envFiles{"r1": {{RepositoryID: "r1", Path: ".env", Content: "API_TOKEN=s3cr3t-value\n"}}}
	svc, worktree := newGitRunner(t, saved)
	// A directory where the file goes: the write fails.
	if err := os.MkdirAll(filepath.Join(worktree, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(worktree, "spawned")
	_, err := svc.Start(context.Background(), "s1", "", "touch "+marker)
	if errs.Code(err) != "ENV_FILE_WRITE_FAILED" {
		t.Fatalf("got %v, want ENV_FILE_WRITE_FAILED", err)
	}
	if strings.Contains(err.Error(), "s3cr3t-value") {
		t.Fatalf("the refusal shows an env file value: %v", err)
	}
	if runs, _ := svc.List(context.Background(), "s1"); len(runs) != 0 {
		t.Fatalf("a refused run is recorded: %+v", runs)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a refused run was spawned")
	}
}
