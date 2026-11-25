package orchestration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The commit check of a shared worktree (ADR 0105): one hooks dir per
// worktree, the committing session read from its own environment at commit
// time, and nothing removed while another session is on the worktree.

// recordingGuard stands in for gitcli.CommitGuard: it keeps each worktree's
// hooks path and the calls it was given.
type recordingGuard struct {
	hooksPath map[string]string
	installs  []string // "<worktree>=<hooksDir>"
	removes   []string
}

func newRecordingGuard() *recordingGuard { return &recordingGuard{hooksPath: map[string]string{}} }

func (g *recordingGuard) Install(_ context.Context, worktree, hooksDir string) (string, error) {
	g.installs = append(g.installs, worktree+"="+hooksDir)
	g.hooksPath[worktree] = hooksDir
	return "", nil
}

func (g *recordingGuard) Remove(_ context.Context, worktree string) error {
	g.removes = append(g.removes, worktree)
	delete(g.hooksPath, worktree)
	return nil
}

func (g *recordingGuard) HooksPath(_ context.Context, worktree string) (string, error) {
	return g.hooksPath[worktree], nil
}

// sharedWorld is a service whose commit check runs against a recording guard
// and a temporary hooks root.
type sharedWorld struct {
	svc   *Service
	guard *recordingGuard
	root  string
}

func newSharedWorld(t *testing.T) *sharedWorld {
	t.Helper()
	svc, _, _ := newTestService(t)
	w := &sharedWorld{svc: svc, guard: newRecordingGuard(), root: t.TempDir()}
	svc.CommitGuard = w.guard
	svc.CommitCheckDir = func(key string) string { return filepath.Join(w.root, key) }
	svc.SessionArchitectureURL = func(id string) string { return "http://harness.test/api/sessions/" + id + "/architecture" }
	svc.WorktreeArchitectureURL = func(ws string) string { return "http://harness.test/api/worktrees/" + ws + "/architecture" }
	return w
}

// worktree makes a worktree folder for workspace id ws.
func (w *sharedWorld) worktree(t *testing.T, ws string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ws)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// record puts a session on the worktree in the store.
func (w *sharedWorld) record(t *testing.T, id, ws, dir string, mut func(*domain.Session)) *domain.Session {
	t.Helper()
	s := &domain.Session{ID: id, ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", WorkspaceID: ws, WorkingDir: dir, Status: domain.SessionRunning}
	if mut != nil {
		mut(s)
	}
	created, err := w.svc.sessions.Create(s)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func (w *sharedWorld) hooksDir(ws string) string { return filepath.Join(w.root, ws) }

func TestTwoSessionsOnOneWorktreeShareOneHooksDirNamedAfterTheWorktree(t *testing.T) {
	w := newSharedWorld(t)
	dir := w.worktree(t, "ws1")
	w.record(t, "sess-a", "ws1", dir, nil)
	w.record(t, "sess-b", "ws1", dir, func(s *domain.Session) { s.Joined = true })

	for _, id := range []string{"sess-a", "sess-b"} {
		if err := w.svc.EnsureCommitCheck(id, "ws1", dir, false); err != nil {
			t.Fatal(err)
		}
	}
	want := dir + "=" + w.hooksDir("ws1")
	for _, got := range w.guard.installs {
		if got != want {
			t.Errorf("install %q, want %q", got, want)
		}
	}
	if len(w.guard.installs) != 2 {
		t.Errorf("installs = %v, want one per call", w.guard.installs)
	}
	if _, err := os.Stat(filepath.Join(w.hooksDir("ws1"), "pre-commit")); err != nil {
		t.Errorf("no pre-commit hook in the worktree's hooks dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.root, "sess-a")); err == nil {
		t.Error("a per-session hooks dir was made")
	}
	for _, id := range []string{"sess-a", "sess-b"} {
		if r := w.svc.commitCheckReason(id); r != "" {
			t.Errorf("%s reads as unguarded: %s", id, r)
		}
	}
}

func TestEndingTheCreatorKeepsTheCheckForTheSessionStillOnTheWorktree(t *testing.T) {
	w := newSharedWorld(t)
	dir := w.worktree(t, "ws1")
	w.record(t, "sess-a", "ws1", dir, nil)
	w.record(t, "sess-b", "ws1", dir, func(s *domain.Session) { s.Joined = true })
	for _, id := range []string{"sess-a", "sess-b"} {
		if err := w.svc.EnsureCommitCheck(id, "ws1", dir, false); err != nil {
			t.Fatal(err)
		}
	}

	w.svc.RemoveCommitCheck("sess-a", "ws1", dir)
	if len(w.guard.removes) != 0 {
		t.Fatalf("the guard removed %v while sess-b is on the worktree", w.guard.removes)
	}
	if _, err := os.Stat(filepath.Join(w.hooksDir("ws1"), "pre-commit")); err != nil {
		t.Fatalf("the hook is gone: %v", err)
	}
	if err := w.svc.EnsureCommitCheck("sess-b", "ws1", dir, true); err != nil || w.svc.commitCheckReason("sess-b") != "" {
		t.Fatalf("sess-b is not guarded: %v %q", err, w.svc.commitCheckReason("sess-b"))
	}

	if err := w.svc.sessions.Delete("sess-a"); err != nil {
		t.Fatal(err)
	}
	w.svc.RemoveCommitCheck("sess-b", "ws1", dir)
	if !slices.Equal(w.guard.removes, []string{dir}) {
		t.Errorf("removes = %v, want one for the worktree", w.guard.removes)
	}
	if _, err := os.Stat(w.hooksDir("ws1")); err == nil {
		t.Error("the hooks dir outlived the last session")
	}
}

func TestTheLastSessionLeavingForGoodRemovesTheCheckWhateverTheOrder(t *testing.T) {
	w := newSharedWorld(t)
	dir := w.worktree(t, "ws1")
	w.record(t, "sess-a", "ws1", dir, nil)
	if err := w.svc.EnsureCommitCheck("sess-a", "ws1", dir, false); err != nil {
		t.Fatal(err)
	}
	w.svc.RemoveCommitCheck("sess-a", "ws1", dir)
	if len(w.guard.removes) != 1 {
		t.Errorf("removes = %v, want 1", w.guard.removes)
	}
}

// hookRun runs the installed pre-commit hook with a fake git and a fake curl
// and answers the URL curl was asked for.
func hookRun(t *testing.T, hooksDir string, env ...string) string {
	t.Helper()
	bin := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "url")
	fake := map[string]string{
		"git":  "#!/bin/sh\nexit 0\n",
		"curl": "#!/bin/sh\nfor a; do last=$a; done\nprintf '%s' \"$last\" > '" + logFile + "'\nprintf '\\n200'\n",
	}
	for name, body := range fake {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", filepath.Join(hooksDir, "pre-commit"))
	cmd.Env = append([]string{"PATH=" + bin + ":/usr/bin:/bin"}, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook: %v\n%s", err, out)
	}
	b, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheHookPostsToTheCommittingSessionsEndpointAndFallsBackToTheWorktrees(t *testing.T) {
	w := newSharedWorld(t)
	dir := w.worktree(t, "ws1")
	w.record(t, "sess-a", "ws1", dir, nil)
	if err := w.svc.EnsureCommitCheck("sess-a", "ws1", dir, false); err != nil {
		t.Fatal(err)
	}
	hooks := w.hooksDir("ws1")

	b := w.svc.SessionArchitectureURL("sess-b")
	if got := hookRun(t, hooks, "HARNESS_ARCHITECTURE_URL="+b); got != b+"/commit" {
		t.Errorf("with session b's environment the hook posted to %q, want %q", got, b+"/commit")
	}
	want := w.svc.WorktreeArchitectureURL("ws1") + "/commit"
	if got := hookRun(t, hooks); got != want {
		t.Errorf("with no session environment the hook posted to %q, want %q", got, want)
	}
	script, _ := os.ReadFile(filepath.Join(hooks, "pre-commit"))
	if strings.Contains(string(script), "sess-a") {
		t.Error("the shared hook names the session that installed it")
	}
}

func TestACommitWithNoSessionEnvironmentIsAlwaysCheckedAsSomeone(t *testing.T) {
	w := newSharedWorld(t)
	dir := w.worktree(t, "ws1")
	prov := w.svc.workspaces.(*fakeProvisioner)
	prov.created = append(prov.created, &domain.Workspace{ID: "ws1", RepositoryID: "r1", Path: dir})
	w.record(t, "creator", "ws1", dir, func(s *domain.Session) { s.Status = domain.SessionStopped })
	w.record(t, "joiner", "ws1", dir, func(s *domain.Session) { s.Joined = true })
	w.record(t, "reader", "ws1", dir, func(s *domain.Session) { s.Joined, s.Access = true, domain.AccessReadOnly })
	ctx := context.Background()
	asked := func() ports.CommitAttribution {
		t.Helper()
		got, err := w.svc.CommitAttribution(ctx, "ws1")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := asked(); got.SessionID != "creator" {
		t.Fatalf("attributed to %+v, want the creator, which still counts once it has ended", got)
	}
	if err := w.svc.sessions.Delete("creator"); err != nil {
		t.Fatal(err)
	}
	if got := asked(); got.SessionID != "joiner" {
		t.Fatalf("attributed to %+v, want the newest write session, never the read-only one", got)
	}
	if err := w.svc.sessions.Delete("joiner"); err != nil {
		t.Fatal(err)
	}
	if got := asked(); got.SessionID != "reader" {
		t.Fatalf("attributed to %+v, want the read-only session: its check refuses the commit, so it never passes unchecked", got)
	}
	if err := w.svc.sessions.Delete("reader"); err != nil {
		t.Fatal(err)
	}
	got := asked()
	if got.SessionID != "" || got.RepositoryID != "r1" || got.Dir != dir {
		t.Fatalf("with no session on the worktree the answer is %+v, want the worktree itself, checked with no agent rights", got)
	}
	if _, err := w.svc.CommitAttribution(ctx, "ws-unknown"); err == nil {
		t.Error("a worktree nobody knows was answered")
	}
}

func TestBootRepairGuardsEachWorktreeOnceAndSweepsWhatNoSessionRecords(t *testing.T) {
	w := newSharedWorld(t)
	d1, d2 := w.worktree(t, "ws1"), w.worktree(t, "ws2")
	w.record(t, "a1", "ws1", d1, nil)
	w.record(t, "a2", "ws1", d1, func(s *domain.Session) { s.Joined = true })
	w.record(t, "b1", "ws2", d2, nil)
	stale := filepath.Join(w.root, "ws-gone")
	oldLayout := filepath.Join(w.root, "a1") // the old layout named it after the session
	for _, d := range []string{stale, oldLayout} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The worktree still points at an old per-session dir.
	w.guard.hooksPath[d1] = oldLayout

	bad, err := w.svc.RepairCommitChecks(context.Background())
	if err != nil || len(bad) != 0 {
		t.Fatalf("repair: %v %v", bad, err)
	}
	if len(w.guard.installs) != 2 {
		t.Errorf("installs = %v, want one per worktree", w.guard.installs)
	}
	if w.guard.hooksPath[d1] != w.hooksDir("ws1") || w.guard.hooksPath[d2] != w.hooksDir("ws2") {
		t.Errorf("worktrees point at %v", w.guard.hooksPath)
	}
	for _, d := range []string{stale, oldLayout} {
		if _, err := os.Stat(d); err == nil {
			t.Errorf("%s was not swept", d)
		}
	}
	for _, id := range []string{"a1", "a2", "b1"} {
		if r := w.svc.commitCheckReason(id); r != "" {
			t.Errorf("%s unguarded after repair: %s", id, r)
		}
	}
}

// failingInstall refuses to point one worktree at its hooks dir.
type failingInstall struct {
	*recordingGuard
	worktree string
}

func (f failingInstall) Install(ctx context.Context, worktree, hooksDir string) (string, error) {
	if worktree == f.worktree {
		return "", errors.New("git config is read-only")
	}
	return f.recordingGuard.Install(ctx, worktree, hooksDir)
}

func TestAnOldPerSessionHooksDirGoesOnlyOnceItsWorktreeIsPointedAtTheNewOne(t *testing.T) {
	w := newSharedWorld(t)
	good, bad := w.worktree(t, "ws-good"), w.worktree(t, "ws-bad")
	w.record(t, "s-good", "ws-good", good, nil)
	w.record(t, "s-bad", "ws-bad", bad, nil)
	w.svc.CommitGuard = failingInstall{w.guard, bad}
	for _, id := range []string{"s-good", "s-bad", "s-gone"} {
		if err := os.MkdirAll(filepath.Join(w.root, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	failed, err := w.svc.RepairCommitChecks(context.Background())
	if err != nil || failed["s-bad"] == nil || failed["s-good"] != nil {
		t.Fatalf("repair: %v %v; want only the bad worktree to fail", failed, err)
	}
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(w.root, name)); return err == nil }
	if exists("s-good") {
		t.Error("the old dir of a session whose worktree was re-pointed is still there")
	}
	if !exists("s-bad") {
		t.Error("the old dir of a session whose worktree could not be re-pointed was removed: its commits would pass unchecked")
	}
	if exists("s-gone") {
		t.Error("the dir of a session with no worktree is still there")
	}
}

func TestAResumeOfASharedWorktreeWhoseHooksFolderWasLostGoesOffForThatSessionAndRepairsForAll(t *testing.T) {
	w := newSharedWorld(t)
	dir := w.worktree(t, "ws1")
	w.record(t, "sess-a", "ws1", dir, nil)
	w.record(t, "sess-b", "ws1", dir, func(s *domain.Session) { s.Joined = true })
	for _, id := range []string{"sess-a", "sess-b"} {
		if err := w.svc.EnsureCommitCheck(id, "ws1", dir, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(w.hooksDir("ws1")); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.EnsureCommitCheck("sess-b", "ws1", dir, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.hooksDir("ws1"), "pre-commit")); err != nil {
		t.Errorf("the hooks folder was not repaired: %v", err)
	}
	if w.svc.commitCheckReason("sess-a") != "" || w.svc.commitCheckReason("sess-b") != "" {
		t.Error("a session reads as off after the repair")
	}
}

func TestASessionsSandboxGrantsItsWorktreesHooksDirAndNoOtherWorktrees(t *testing.T) {
	w := newSandboxWorld(t)
	guard := newRecordingGuard()
	root := t.TempDir()
	w.svc.CommitGuard = guard
	w.svc.CommitCheckDir = func(key string) string { return filepath.Join(root, key) }
	w.svc.SessionArchitectureURL = func(id string) string { return "http://harness.test/" + id }

	sess, p := w.start(t, "implement")
	own := filepath.Join(root, sess.WorkspaceID)
	if !slices.Contains(p.Readable, own) || slices.Contains(p.Writable, own) {
		t.Errorf("hooks dir %s not read-only: readable %v writable %v", own, p.Readable, p.Writable)
	}
	if slices.Contains(p.Readable, filepath.Join(root, sess.ID)) {
		t.Error("the session's own id names a hooks dir")
	}
	if slices.Contains(p.Readable, filepath.Join(root, "ws-other")) {
		t.Error("another worktree's hooks dir is granted")
	}
}
