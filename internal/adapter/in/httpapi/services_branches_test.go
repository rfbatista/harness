package httpapi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/commandrunner"
	"operators-mcp/internal/adapter/out/containerrunner/containerrunnertest"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Running a service on a branch (ADR 0080): a real git repository at the
// repository's root, a bare "origin", and a real `sh -c`.

// realGit is the git binary with the harness's shim directories off PATH: an
// agent session's shim would gate the branch commands these tests run.
var realGit = func() string {
	var keep []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if base := filepath.Base(dir); strings.HasPrefix(base, "harness-shims-") || strings.HasPrefix(base, "harness-keychain-shim-") {
			continue
		}
		keep = append(keep, dir)
	}
	for _, dir := range keep {
		if p := filepath.Join(dir, "git"); isExecutable(p) {
			return p
		}
	}
	return "git"
}()

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// gitRepo is a git repository the test drives with the git binary.
type gitRepo struct {
	t   *testing.T
	dir string
	at  time.Time
}

func (g *gitRepo) git(args ...string) string {
	g.t.Helper()
	cmd := exec.Command(realGit, append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.t.Fatalf("git %v in %s: %v\n%s", args, g.dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes file with content and commits it one minute after the last
// commit, so branch order is known.
func (g *gitRepo) commit(file, content string) string {
	g.t.Helper()
	g.at = g.at.Add(time.Minute)
	path := filepath.Join(g.dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		g.t.Fatal(err)
	}
	g.git("add", "-A")
	date := g.at.Format(time.RFC3339)
	cmd := exec.Command(realGit, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "change "+file)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		g.t.Fatalf("commit: %v\n%s", err, out)
	}
	return g.git("rev-parse", "HEAD")
}

func initGitRepo(t *testing.T, dir string) *gitRepo {
	t.Helper()
	g := &gitRepo{t: t, dir: dir, at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	g.git("init", "-q", "-b", "main")
	g.commit("README", "hello\n")
	return g
}

// withOrigin makes a bare repository, sets it as origin and pushes main.
func (g *gitRepo) withOrigin() *gitRepo {
	g.t.Helper()
	bare := &gitRepo{t: g.t, dir: g.t.TempDir()}
	bare.git("init", "-q", "--bare", "-b", "main")
	g.git("remote", "add", "origin", bare.dir)
	g.git("push", "-q", "origin", "main")
	return bare
}

func gitFixture(t *testing.T, opts services.Options) (*servicesFixture, *gitRepo) {
	t.Helper()
	if opts.Runners == nil {
		opts.Runners = map[domain.ServiceKind]ports.ServiceProcessRunner{
			domain.ServiceKindCommand: commandrunner.Runner{}, domain.ServiceKindImage: &containerrunnertest.Fake{},
		}
	}
	if opts.WorktreeRoot != "" {
		opts.WorktreeRoot = filepath.Clean(opts.WorktreeRoot)
	}
	opts.BranchGit = gitcli.NewServiceBranches(gitcli.WithGit(realGit))
	opts.StopGrace = 5 * time.Second
	f := newServicesFixture(t, opts)
	return f, initGitRepo(t, f.repoDir())
}

func (f *servicesFixture) branches(id, query string) (int, map[string]any) {
	f.t.Helper()
	return f.call("GET", "/api/list_service_branches?service_id="+id+"&query="+query, nil)
}

func branchNames(out map[string]any) []string {
	var names []string
	for _, b := range out["branches"].([]any) {
		m := b.(map[string]any)
		name := m["name"].(string)
		if m["remote"] == true {
			name = "origin/" + name
		}
		names = append(names, name)
	}
	return names
}

func TestServicesBranches_LocalThenRemoteEachNewestFirstWithTheirTip(t *testing.T) {
	f, repo := gitFixture(t, services.Options{})
	bare := repo.withOrigin()
	repo.git("checkout", "-q", "-b", "old")
	repo.commit("old.txt", "old")
	repo.git("checkout", "-q", "-b", "feature/new")
	newest := repo.commit("new.txt", "new")
	repo.git("checkout", "-q", "main")
	repo.git("push", "-q", "origin", "old", "feature/new")
	repo.git("fetch", "-q", "origin")
	// A branch that only origin has, never fetched, is not listed.
	other := &gitRepo{t: t, dir: t.TempDir(), at: repo.at}
	other.git("clone", "-q", bare.dir, other.dir+"/c")
	other.dir += "/c"
	other.git("checkout", "-q", "-b", "unfetched")
	other.commit("u.txt", "u")
	other.git("push", "-q", "origin", "unfetched")

	id := f.command("web", "true")
	code, out := f.branches(id, "")
	if code != 200 {
		t.Fatalf("list = %d %v", code, out)
	}
	want := []string{"feature/new", "old", "main", "origin/feature/new", "origin/old", "origin/main"}
	if got := branchNames(out); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("branches = %v, want %v", got, want)
	}
	first := out["branches"].([]any)[0].(map[string]any)
	if first["sha"] != newest[:len(first["sha"].(string))] || len(first["sha"].(string)) < 7 || first["subject"] != "change new.txt" {
		t.Fatalf("first = %v (tip %s)", first, newest)
	}
	if at, err := time.Parse(time.RFC3339, first["committed_at"].(string)); err != nil || at.IsZero() {
		t.Fatalf("committed_at = %v", first["committed_at"])
	}
	if out["truncated"] != false {
		t.Fatalf("truncated = %v", out["truncated"])
	}
	if remote := out["branches"].([]any)[3].(map[string]any); remote["remote"] != true || remote["name"] != "feature/new" {
		t.Fatalf("a remote branch has its name without origin/: %v", remote)
	}
}

func TestServicesBranches_TheQueryFiltersByASubstringIgnoringCase(t *testing.T) {
	f, repo := gitFixture(t, services.Options{})
	repo.git("branch", "Feature/Login")
	repo.git("branch", "fix/typo")
	id := f.command("web", "true")

	_, out := f.branches(id, "LOGIN")
	if got := branchNames(out); strings.Join(got, ",") != "Feature/Login" {
		t.Fatalf("filtered = %v", got)
	}
	_, out = f.branches(id, "zzz")
	if got, ok := out["branches"].([]any); !ok || len(got) != 0 || out["truncated"] != false {
		t.Fatalf("no match = %v", out)
	}
}

func TestServicesBranches_AtMost200AreReturnedAndTruncatedSaysThereWereMore(t *testing.T) {
	f, repo := gitFixture(t, services.Options{})
	id := f.command("web", "true")
	// 199 branches beside main is exactly 200.
	for i := 0; i < 199; i++ {
		repo.git("branch", fmt.Sprintf("b%03d", i))
	}
	_, out := f.branches(id, "")
	if n := len(out["branches"].([]any)); n != 200 || out["truncated"] != false {
		t.Fatalf("exactly 200 = %d truncated %v", n, out["truncated"])
	}
	repo.git("branch", "b200")
	_, out = f.branches(id, "")
	if n := len(out["branches"].([]any)); n != 200 || out["truncated"] != true {
		t.Fatalf("201 = %d truncated %v", n, out["truncated"])
	}
}

func TestServicesBranches_NothingToListForAnImageAServiceWithNoRepositoryOrAnUnknownService(t *testing.T) {
	f, _ := gitFixture(t, services.Options{})
	imageID := f.createID(imageInput("db"))
	looseID := f.createID(commandInput("loose"))
	for _, id := range []string{imageID, looseID} {
		code, out := f.branches(id, "")
		list, ok := out["branches"].([]any)
		if code != 200 || !ok || len(list) != 0 || out["truncated"] != false {
			t.Fatalf("list = %d %v", code, out)
		}
	}
	code, out := f.branches("nope", "")
	if c, _ := errorOf(out); code != 404 || c != "SERVICE_NOT_FOUND" {
		t.Fatalf("unknown = %d %v", code, out)
	}
}
