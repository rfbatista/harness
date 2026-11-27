package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func protectedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"check.sh", "main.go", ".github/workflows/ci.yml", ".github/workflows/release.yml", ".github/ISSUE_TEMPLATE/bug.md", "ci/a.yml", "ci/sub/b.yml"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func rels(root string, paths []string) []string {
	var out []string
	for _, p := range paths {
		r, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func TestTheSandboxProtectsTheDefaultGlobsAsPathsAndAScopeUnlocksItsOwn(t *testing.T) {
	root := protectedTree(t)
	globs := domain.DefaultProtectedPaths()

	got := rels(root, ProtectedSandboxPaths(root, globs, nil))
	for _, want := range []string{"check.sh", ".github/workflows", ".gitlab-ci.yml", "Jenkinsfile", ".circleci"} {
		if !slices.Contains(got, want) {
			t.Errorf("%q is not protected: %v", want, got)
		}
	}
	// A name that does not exist yet is protected, so creating it is refused too;
	// an unprotected path is not named.
	if slices.Contains(got, "main.go") || slices.Contains(got, ".github/ISSUE_TEMPLATE") {
		t.Errorf("an unprotected path is protected: %v", got)
	}

	// A scope that unlocks check.sh leaves it out and the rest in.
	got = rels(root, ProtectedSandboxPaths(root, globs, []string{"check.sh"}))
	if slices.Contains(got, "check.sh") || !slices.Contains(got, ".github/workflows") {
		t.Errorf("scope check.sh: %v", got)
	}

	// A scope inside a protected folder unlocks that file: the folder's other
	// files are listed one by one.
	got = rels(root, ProtectedSandboxPaths(root, globs, []string{".github/workflows/ci.yml"}))
	if slices.Contains(got, ".github/workflows") || slices.Contains(got, ".github/workflows/ci.yml") || !slices.Contains(got, ".github/workflows/release.yml") {
		t.Errorf("scope inside a folder: %v", got)
	}

	// A scope over the whole folder unlocks it.
	got = rels(root, ProtectedSandboxPaths(root, globs, []string{".github/workflows/**"}))
	if slices.ContainsFunc(got, func(p string) bool { return p == ".github/workflows" || filepath.Dir(p) == ".github/workflows" }) {
		t.Errorf("scope over the folder: %v", got)
	}
}

func TestAWildcardGlobProtectsTheFilesThatMatchItNow(t *testing.T) {
	root := protectedTree(t)
	got := rels(root, ProtectedSandboxPaths(root, []string{"ci/**/*.yml", "*.go"}, nil))
	want := []string{"ci/a.yml", "ci/sub/b.yml", "main.go"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if ProtectedSandboxPaths("", []string{"check.sh"}, nil) != nil {
		t.Error("no worktree protects nothing")
	}
}

func TestAGatedSessionsPolicyCarriesTheProtectedPathsReadOnlyInsideItsWritableWorktreeAndAScopedOneDoesNot(t *testing.T) {
	root := protectedTree(t)
	build := func(scope ...string) ports.SandboxPolicy {
		return BuildSandboxPolicy(SandboxSession{
			Mode: domain.SessionModeImplement, Worktree: root, TempDir: t.TempDir(),
			Protected: ProtectedSandboxPaths(root, domain.DefaultProtectedPaths(), scope),
		})
	}
	check := filepath.Join(root, "check.sh")
	policy := build()
	if !slices.Contains(policy.Writable, root) || !slices.Contains(policy.Readable, check) || slices.Contains(policy.Writable, check) {
		t.Errorf("a protected path is not read-only inside the writable worktree: readable %v writable %v", policy.Readable, policy.Writable)
	}
	if !slices.Contains(policy.Readable, filepath.Join(root, ".github", "workflows")) {
		t.Errorf("the protected folder is not read-only: %v", policy.Readable)
	}
	if main := filepath.Join(root, "main.go"); slices.Contains(policy.Readable, main) {
		t.Error("an unprotected file is read-only")
	}

	scoped := build("check.sh")
	if slices.Contains(scoped.Readable, check) {
		t.Errorf("a scope-unlocked path is still read-only: %v", scoped.Readable)
	}
	if !slices.Contains(scoped.Readable, filepath.Join(root, ".github", "workflows")) {
		t.Error("a scope that covers one path unlocked another")
	}
	if !slices.Contains(scoped.Writable, root) {
		t.Error("the worktree is no longer writable")
	}

	// A session that is not given protection has no such entries.
	plain := BuildSandboxPolicy(SandboxSession{Mode: domain.SessionModeImplement, Worktree: root, TempDir: t.TempDir()})
	if slices.Contains(plain.Readable, check) {
		t.Error("an unprotected session has check.sh read-only")
	}
}

// memGuard keeps guard configurations in memory.
type memGuard map[string]domain.GuardConfig

func (m memGuard) Keep(_ context.Context, id string, c domain.GuardConfig) error {
	m[id] = c
	return nil
}
func (m memGuard) Config(_ context.Context, id string) (domain.GuardConfig, bool, error) {
	c, ok := m[id]
	return c, ok, nil
}
func (m memGuard) CountRed(context.Context, string) (int, error) { return 0, nil }
func (m memGuard) MarkEscalated(context.Context, string) error   { return nil }
func (m memGuard) ResumeRed(context.Context, string) error       { return nil }
func (m memGuard) ResetRed(context.Context, string) error        { return nil }

func TestAStartedSessionsSandboxPolicyProtectsItsPathsUnlessItsScopeOrItsModeSaysOtherwiseAndAResumeKeepsTheScope(t *testing.T) {
	protected := func(p ports.SandboxPolicy, wt, rel string) bool {
		return slices.Contains(p.Readable, filepath.Join(wt, rel))
	}

	// A gated mode: check.sh and the workflows folder are read-only in the worktree.
	w := newSandboxWorld(t)
	sess, p := w.start(t, "implement")
	if !protected(p, sess.WorkingDir, "check.sh") || !protected(p, sess.WorkingDir, ".github/workflows") || !slices.Contains(p.Writable, sess.WorkingDir) {
		t.Errorf("implement policy: readable %v writable %v", p.Readable, p.Writable)
	}

	// A mode that is not gated is not protected.
	w = newSandboxWorld(t)
	sess, p = w.start(t, "design")
	if protected(p, sess.WorkingDir, "check.sh") {
		t.Errorf("a design session has check.sh read-only: %v", p.Readable)
	}

	// A scope unlocks its path for that session, and a resume takes it again.
	w = newSandboxWorld(t)
	w.svc.GuardSessions = memGuard{}
	sess, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI, Scope: []string{"check.sh"}})
	if err != nil {
		t.Fatal(err)
	}
	p = w.driver.Calls()[0].Policy
	if protected(p, sess.WorkingDir, "check.sh") || !protected(p, sess.WorkingDir, ".github/workflows") {
		t.Errorf("scoped policy: readable %v", p.Readable)
	}
	if err := w.svc.sessions.UpdateStatus(sess.ID, domain.SessionDone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	calls := w.driver.Calls()
	if resumed := calls[len(calls)-1].Policy; protected(resumed, sess.WorkingDir, "check.sh") || !protected(resumed, sess.WorkingDir, ".github/workflows") {
		t.Errorf("resumed policy: readable %v", resumed.Readable)
	}
}

func TestASandboxedSessionCannotReachTheDatabaseOrTheKeyButStillWritesItsTaskMemoryInTheDataDir(t *testing.T) {
	data := t.TempDir()
	memory := filepath.Join(data, "task-memory", "task1", "repo1")
	secrets := []string{filepath.Join(data, "harness.db"), filepath.Join(data, "harness.db-wal"), filepath.Join(data, "harness.db-shm"), filepath.Join(data, "secret.key")}
	policy := BuildSandboxPolicy(SandboxSession{
		Mode: domain.SessionModeImplement, Worktree: t.TempDir(), TempDir: t.TempDir(),
		Allow:   []ports.SandboxPath{{Path: memory, Write: true}},
		Secrets: secrets,
	})
	if !slices.Contains(policy.Writable, memory) {
		t.Errorf("the task-memory folder is not writable: %v", policy.Writable)
	}
	for _, s := range secrets {
		if !slices.Contains(policy.Secrets, s) {
			t.Errorf("%s is not a secret of the policy: %v", s, policy.Secrets)
		}
		if slices.Contains(policy.Readable, s) || slices.Contains(policy.Writable, s) {
			t.Errorf("%s is granted", s)
		}
	}
	// The data directory itself is not hidden, nor granted: only what is named.
	if slices.Contains(policy.Hidden, data) {
		t.Error("the whole data directory is hidden")
	}

	// A grant that covers a secret does not reopen it.
	wide := BuildSandboxPolicy(SandboxSession{Mode: domain.SessionModeImplement, Worktree: t.TempDir(), TempDir: t.TempDir(),
		Allow: []ports.SandboxPath{{Path: data, Write: true}}, Secrets: secrets})
	if !slices.Contains(wide.Secrets, secrets[0]) {
		t.Errorf("a secret under a granted folder is not kept as a secret: %v", wide.Secrets)
	}
}
