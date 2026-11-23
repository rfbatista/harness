package orchestration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/archrules"
	"operators-mcp/internal/application/repoarch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Real git, real worktrees and the real pre-commit hook, in a temporary
// repository (the spec's service-level seams, taken one step further: what a
// fake git cannot prove). Only the harness's HTTP endpoint is a stand-in: it
// answers like handleWorktreeArchitectureCommit and handleArchitectureCommit
// do, through the real Service.CommitAttribution and the real archrules.

func realGitBin(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"/opt/homebrew/bin/git", "/usr/bin/git"} {
		if out, err := exec.Command(p, "--version").CombinedOutput(); err == nil && strings.HasPrefix(string(out), "git version") {
			return p
		}
	}
	t.Skip("no real git binary")
	return ""
}

type realGit struct {
	t   *testing.T
	bin string
}

func (g realGit) environ(extra ...string) []string {
	return append([]string{
		"PATH=" + filepath.Dir(g.bin) + ":/usr/bin:/bin", "HOME=" + g.t.TempDir(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	}, extra...)
}

func (g realGit) run(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command(g.bin, args...)
	cmd.Dir = dir
	cmd.Env = g.environ(env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (g realGit) must(dir string, env []string, args ...string) string {
	g.t.Helper()
	out, err := g.run(dir, env, args...)
	if err != nil {
		g.t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return out
}

// commitCall is what the stand-in endpoint saw: who the commit was checked as
// and which staged paths.
type commitCall struct {
	session, via, paths string
	denied              bool
}

type realWorld struct {
	*sharedWorld
	git   realGit
	repo  string
	wt    string
	mu    sync.Mutex
	calls []commitCall
	url   string
}

func (r *realWorld) last() commitCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return commitCall{}
	}
	return r.calls[len(r.calls)-1]
}

type noRepoRecords struct{}

func (noRepoRecords) Current(context.Context, string) (*repoarch.Current, error) { return nil, nil }

func newRealWorld(t *testing.T) *realWorld {
	t.Helper()
	g := realGit{t: t, bin: realGitBin(t)}
	w := newSharedWorld(t)
	r := &realWorld{sharedWorld: w, git: g, repo: t.TempDir()}
	w.svc.CommitGuard = gitcli.NewCommitGuard(gitcli.WithGit(g.bin))

	g.must(r.repo, nil, "init", "-q", "-b", "main")
	g.must(r.repo, nil, "commit", "-q", "--allow-empty", "-m", "base")
	r.wt = filepath.Join(t.TempDir(), "wt")
	g.must(r.repo, nil, "worktree", "add", "-q", "-b", "task/shared", r.wt)

	rules := archrules.NewService(archrules.Deps{Records: noRepoRecords{}, Sessions: w.svc.sessions})
	check := func(rw http.ResponseWriter, req *http.Request, session, via string) {
		body := make([]byte, 4096)
		n, _ := req.Body.Read(body)
		call := commitCall{session: session, via: via, paths: strings.ReplaceAll(string(body[:n]), "\x00", ",")}
		v := rules.CheckArchitecture(req.Context(), ports.ArchitectureCheck{SessionID: session, Point: domain.PointBeforeCommit, Tool: "git commit"})
		call.denied = v.Deny != ""
		r.mu.Lock()
		r.calls = append(r.calls, call)
		r.mu.Unlock()
		if call.denied {
			http.Error(rw, v.Deny, http.StatusConflict)
			return
		}
		rw.WriteHeader(http.StatusOK)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{id}/architecture/commit", func(rw http.ResponseWriter, req *http.Request) {
		check(rw, req, req.PathValue("id"), "session")
	})
	mux.HandleFunc("POST /api/worktrees/{id}/architecture/commit", func(rw http.ResponseWriter, req *http.Request) {
		who, err := w.svc.CommitAttribution(req.Context(), req.PathValue("id"))
		if err != nil {
			http.Error(rw, err.Error(), http.StatusNotFound)
			return
		}
		check(rw, req, who.SessionID, "worktree")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	r.url = srv.URL
	w.svc.SessionArchitectureURL = func(id string) string { return srv.URL + "/api/sessions/" + id + "/architecture" }
	w.svc.WorktreeArchitectureURL = func(ws string) string { return srv.URL + "/api/worktrees/" + ws + "/architecture" }
	return r
}

// commit stages a new file in the worktree and commits it as the session whose
// environment names it ("" is a commit with no session environment).
func (r *realWorld) commit(t *testing.T, session, file string) (string, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.wt, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git.must(r.wt, nil, "add", file)
	var env []string
	if session != "" {
		env = []string{"HARNESS_ARCHITECTURE_URL=" + r.svc.SessionArchitectureURL(session)}
	}
	return r.git.run(r.wt, env, "commit", "-q", "-m", "add "+file)
}

func (r *realWorld) shared(t *testing.T) {
	t.Helper()
	r.record(t, "sess-a", "ws1", r.wt, func(s *domain.Session) { s.Branch = "task/shared" })
	r.record(t, "sess-b", "ws1", r.wt, func(s *domain.Session) { s.Joined = true; s.Branch = "task/shared" })
	for _, id := range []string{"sess-a", "sess-b"} {
		if err := r.svc.EnsureCommitCheck(id, "ws1", r.wt, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRealGit_TwoSessionsOnOneWorktreeEachCommitCheckedAndAttributedToItself(t *testing.T) {
	r := newRealWorld(t)
	r.shared(t)

	if out, err := r.commit(t, "sess-a", "a.txt"); err != nil {
		t.Fatalf("a's commit: %v\n%s", err, out)
	}
	if c := r.last(); c.session != "sess-a" || c.via != "session" || c.paths != "a.txt," {
		t.Errorf("a's commit was checked as %+v", c)
	}
	if out, err := r.commit(t, "sess-b", "b.txt"); err != nil {
		t.Fatalf("b's commit: %v\n%s", err, out)
	}
	if c := r.last(); c.session != "sess-b" || c.via != "session" || c.paths != "b.txt," {
		t.Errorf("b's commit was checked as %+v", c)
	}
	// The person's own terminal: no session environment, attributed to the
	// session that cut the worktree.
	if out, err := r.commit(t, "", "p.txt"); err != nil {
		t.Fatalf("person's commit: %v\n%s", err, out)
	}
	if c := r.last(); c.session != "sess-a" || c.via != "worktree" {
		t.Errorf("a commit with no session environment was checked as %+v, want sess-a via the worktree endpoint", c)
	}
	if n := len(r.calls); n != 3 {
		t.Errorf("%d checks for 3 commits", n)
	}
	// One hooks dir for the worktree, none per session.
	if _, err := os.Stat(filepath.Join(r.root, "sess-a")); err == nil {
		t.Error("a per-session hooks dir exists")
	}
	if got := strings.TrimSpace(r.git.must(r.wt, nil, "config", "--worktree", "core.hooksPath")); got != r.hooksDir("ws1") {
		t.Errorf("core.hooksPath = %q, want %q", got, r.hooksDir("ws1"))
	}
}

func TestRealGit_ABlockingVerdictFailsTheCommitAndLeavesTheIndexUncommitted(t *testing.T) {
	r := newRealWorld(t)
	r.shared(t)
	r.record(t, "sess-ro", "ws1", r.wt, func(s *domain.Session) { s.Joined = true; s.Access = domain.AccessReadOnly; s.Branch = "task/shared" })
	before := strings.TrimSpace(r.git.must(r.wt, nil, "rev-parse", "HEAD"))
	out, err := r.commit(t, "sess-ro", "ro.txt")
	if err == nil {
		t.Fatalf("a read-only session's commit went through:\n%s", out)
	}
	if !strings.Contains(out, "read-only access") {
		t.Errorf("the refusal does not say why:\n%s", out)
	}
	if after := strings.TrimSpace(r.git.must(r.wt, nil, "rev-parse", "HEAD")); after != before {
		t.Errorf("HEAD moved from %s to %s", before, after)
	}
}

func TestRealGit_WhenTheCreatorEndsOrIsArchivedTheOthersNextCommitIsStillChecked(t *testing.T) {
	r := newRealWorld(t)
	r.shared(t)
	if out, err := r.commit(t, "sess-a", "a.txt"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	if err := r.svc.sessions.UpdateStatus("sess-a", domain.SessionStopped); err != nil {
		t.Fatal(err)
	}
	// Archive: the session's part in the commit check goes for good.
	r.svc.RemoveCommitCheck("sess-a", "ws1", r.wt)
	n := len(r.calls)
	if out, err := r.commit(t, "sess-b", "b.txt"); err != nil {
		t.Fatalf("b's commit after a's archive: %v\n%s", err, out)
	}
	if len(r.calls) != n+1 || r.last().session != "sess-b" {
		t.Errorf("b's commit after a's archive was not checked: %+v", r.calls)
	}
	// A commit with no session environment still finds a session to be
	// attributed to (the stopped creator while it is recorded).
	if out, err := r.commit(t, "", "p.txt"); err != nil {
		t.Fatalf("person's commit: %v\n%s", err, out)
	}
	if r.last().via != "worktree" {
		t.Errorf("the person's commit was not checked: %+v", r.last())
	}

	// The last session leaving removes the guard and the hooks dir; git then
	// uses the repository's own hooks.
	if err := r.svc.sessions.Delete("sess-a"); err != nil {
		t.Fatal(err)
	}
	r.svc.RemoveCommitCheck("sess-b", "ws1", r.wt)
	if _, err := os.Stat(r.hooksDir("ws1")); err == nil {
		t.Error("the hooks dir outlived the last session")
	}
	if out, err := r.git.run(r.wt, nil, "config", "--worktree", "core.hooksPath"); err == nil {
		t.Errorf("core.hooksPath is still set to %q", strings.TrimSpace(out))
	}
}

func TestRealGit_BootRepairMovesAnOldPerSessionHooksDirToTheWorktreesAndSweepsTheOldOnes(t *testing.T) {
	r := newRealWorld(t)
	r.shared(t)
	// The layout before ADR 0105: one dir per session, the worktree pointed at
	// session a's, with a stale hook in it.
	old := filepath.Join(r.root, "sess-a")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.git.must(r.wt, nil, "config", "--worktree", "core.hooksPath", old)
	orphan := filepath.Join(r.root, "sess-gone")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}

	failed, err := r.svc.RepairCommitChecks(context.Background())
	if err != nil || len(failed) != 0 {
		t.Fatalf("repair: %v %v", err, failed)
	}
	if got := strings.TrimSpace(r.git.must(r.wt, nil, "config", "--worktree", "core.hooksPath")); got != r.hooksDir("ws1") {
		t.Errorf("core.hooksPath = %q, want the worktree's dir %q", got, r.hooksDir("ws1"))
	}
	for _, d := range []string{old, orphan} {
		if _, err := os.Stat(d); err == nil {
			t.Errorf("old dir %s survived the sweep", d)
		}
	}
	if out, err := r.commit(t, "sess-b", "b.txt"); err != nil || r.last().session != "sess-b" {
		t.Errorf("a commit after the repair: %v\n%s\n%+v", err, out, r.calls)
	}
}

func TestRealGit_AReadOnlyViewsGitReadsWorkWithOptionalLocksOffAndAWriteIsRefused(t *testing.T) {
	g := realGit{t: t, bin: realGitBin(t)}
	repo := t.TempDir()
	g.must(repo, nil, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.must(repo, nil, "add", "f.txt")
	g.must(repo, nil, "commit", "-q", "-m", "one")
	// Make the file's stat stale so a plain `git status` wants to refresh the
	// index, then take the git dir's write bit away, as the read-only sandbox does.
	if err := os.Chtimes(filepath.Join(repo, "f.txt"), time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo, ".git"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(repo, ".git"), 0o755) })

	if out, err := g.run(repo, nil, "status", "--short"); err == nil {
		t.Logf("plain git status works in the read-only git dir here, so the check below proves less: %s", out)
	}
	env := []string{"GIT_OPTIONAL_LOCKS=0"}
	for _, args := range [][]string{{"status", "--short"}, {"log", "--oneline", "-3"}, {"diff"}, {"diff", "--stat", "HEAD"}} {
		if out, err := g.run(repo, env, args...); err != nil {
			t.Errorf("git %v with GIT_OPTIONAL_LOCKS=0 in a read-only git dir: %v\n%s", args, err, out)
		}
	}
	// The preset the harness hands a read-only session.
	var cfg llmkit.SessionConfig
	withReadOnlyAccess(&cfg, domain.AccessReadOnly, "task/shared")
	if !slices.Contains(cfg.Env, "GIT_OPTIONAL_LOCKS=0") {
		t.Errorf("a read-only session's environment %v lacks GIT_OPTIONAL_LOCKS=0", cfg.Env)
	}
	var rw llmkit.SessionConfig
	withReadOnlyAccess(&rw, domain.AccessWrite, "task/shared")
	if slices.Contains(rw.Env, "GIT_OPTIONAL_LOCKS=0") {
		t.Error("a write session got GIT_OPTIONAL_LOCKS=0")
	}
	// A commit cannot write into that git dir.
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := g.run(repo, env, "commit", "-aq", "-m", "two"); err == nil {
		t.Errorf("a commit into a read-only git dir succeeded:\n%s", out)
	}
}

func TestRealGit_ATaskWorktreesListLeavesOutAFolderRemovedFromDisk(t *testing.T) {
	r := newRealWorld(t)
	svc := r.svc
	gone := filepath.Join(t.TempDir(), "gone")
	r.git.must(r.repo, nil, "worktree", "add", "-q", "-b", "task/gone", gone)
	ws := &domain.Workspace{ID: "ws-live", RepositoryID: "r1", Branch: "task/shared", Path: r.wt}
	wsGone := &domain.Workspace{ID: "ws-gone", RepositoryID: "r1", Branch: "task/gone", Path: gone}
	prov, ok := svc.workspaces.(*fakeProvisioner)
	if !ok {
		t.Skip("workspaces is not the fake provisioner")
	}
	prov.created = append(prov.created, ws, wsGone)
	for _, s := range []*domain.Session{
		{ID: "s-live", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", WorkspaceID: "ws-live", WorkingDir: r.wt, Status: domain.SessionRunning},
		{ID: "s-gone", ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", WorkspaceID: "ws-gone", WorkingDir: gone, Status: domain.SessionStopped},
	} {
		if _, err := svc.sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	ids := func() []string {
		v, err := svc.TaskWorktreeChoices(context.Background(), "tk1", "r1")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, w := range v.Worktrees {
			out = append(out, w.WorktreeID)
		}
		return out
	}
	if got := strings.Join(ids(), ","); got != "ws-gone,ws-live" && got != "ws-live,ws-gone" {
		t.Fatalf("before the removal, listed %q", got)
	}
	r.git.must(r.repo, nil, "worktree", "remove", "--force", gone)
	if got := strings.Join(ids(), ","); got != "ws-live" {
		t.Errorf("after the folder was removed, listed %q, want ws-live only", got)
	}
}
