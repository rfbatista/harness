//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The commit check hooks dir (ADR 0040): one stable folder per session in the
// data directory, named by the worktree's core.hooksPath, kept while the
// session may resume and removed when it ends for good.

// transcriptsThatResume lets every ended session resume.
type transcriptsThatResume struct{}

func (transcriptsThatResume) CanResume(string, string) error { return nil }

func hooksPathOf(t *testing.T, worktree string) string {
	t.Helper()
	out, err := gitIn(worktree, "config", "--get", "core.hooksPath")
	if err != nil {
		t.Fatalf("the worktree has no core.hooksPath: %v %s", err, out)
	}
	return out
}

func TestASessionStartPointsItsWorktreeIntoTheCommitCheckHooksDirInTheDataDir(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("go-developer")

	want := filepath.Join(h.dataDir, "githooks", a.id)
	if got := hooksPathOf(t, a.worktree); got != want {
		t.Fatalf("core.hooksPath = %q, want %q", got, want)
	}
	pointCommitCheckAt(t, a.worktree, h.srv.URL)
	out, err := stageAndCommit(t, a.worktree, map[string]string{"docs/notes.md": "hi\n"})
	if err == nil || !strings.Contains(out, "Architecture rule stay-in-your-layer (blocking)") {
		t.Errorf("a commit outside the layers = %v, %s; want it refused by the rule", err, out)
	}
}

func TestAResumeRewritesTheCommitCheckHooksDirAtTheSamePathAndKeepsTheRepositorysHooks(t *testing.T) {
	h := newArchHooks(t, "blocking")
	h.orch.Transcripts = transcriptsThatResume{}
	own := filepath.Join(h.repoDir, ".git", "hooks")
	if err := os.WriteFile(filepath.Join(own, "pre-commit"), []byte("#!/bin/sh\necho own-pre-commit-ran >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "commit-msg"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := h.as("software-architect")
	dir := hooksPathOf(t, a.worktree)

	if _, err := h.orch.EndInteractive(h.ctx, a.id, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := hooksPathOf(t, a.worktree); got != dir {
		t.Fatalf("a stopped session's core.hooksPath = %q, want it kept at %q", got, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "pre-commit")); err != nil {
		t.Fatalf("a stopped session lost its hooks dir: %v", err)
	}
	// Lose the folder's content, as a clean-up of the data dir would.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	if _, _, err := h.orch.ResumeInteractive(h.ctx, ports.ResumeRequest{SessionID: a.id}); err != nil {
		t.Fatal(err)
	}
	if got := hooksPathOf(t, a.worktree); got != dir {
		t.Fatalf("after resume core.hooksPath = %q, want %q", got, dir)
	}
	pointCommitCheckAt(t, a.worktree, h.srv.URL)
	out, err := stageAndCommit(t, a.worktree, map[string]string{"internal/domain/zone.go": "package domain\n"})
	if err != nil || !strings.Contains(out, "own-pre-commit-ran") {
		t.Errorf("commit = %v, %s; want it to pass and run the repository's own pre-commit", err, out)
	}
	if _, err := os.Lstat(filepath.Join(dir, "commit-msg")); err != nil {
		t.Errorf("the repository's commit-msg hook is not chained: %v", err)
	}
	// Resuming again must not chain the folder to its own earlier copy.
	if _, err := h.orch.EndInteractive(h.ctx, a.id, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.orch.ResumeInteractive(h.ctx, ports.ResumeRequest{SessionID: a.id}); err != nil {
		t.Fatal(err)
	}
	script, _ := os.ReadFile(filepath.Join(dir, "pre-commit"))
	if strings.Contains(string(script), dir) {
		t.Errorf("the hook runs its own folder:\n%s", script)
	}
}

func TestDeletingASessionRemovesItsCommitCheckHooksDir(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("go-developer")
	dir := hooksPathOf(t, a.worktree)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	if err := h.orch.Delete(h.ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the hooks dir of a deleted session is still there: %v", err)
	}
}

// aRestartedHarness is an architecture-hooks server over a database file, so
// its sessions outlive a restart.
func aRestartedHarness(t *testing.T) *archHooks {
	t.Helper()
	h := &archHooks{shellE2E: newShellE2EWith(t, "data.db")}
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		if _, err := h.cat.Agents.CreateAgent(h.ctx, a, "", "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	h.task = h.shellE2E.task("Restart")
	h.save(func(map[string]any) {})
	return h
}

func TestAWorktreeLeftOnADeletedTemporaryFolderIsRepairedWhenTheServerBoots(t *testing.T) {
	h := aRestartedHarness(t)
	a := h.as("go-developer")
	gone := filepath.Join(t.TempDir(), "harness-githooks-1")
	if out, err := gitIn(a.worktree, "config", "--worktree", "core.hooksPath", gone); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(h.dataDir, "githooks", a.id)); err != nil {
		t.Fatal(err)
	}

	h.restart()

	want := filepath.Join(h.dataDir, "githooks", a.id)
	if got := hooksPathOf(t, a.worktree); got != want {
		t.Fatalf("core.hooksPath after the boot = %q, want %q", got, want)
	}
	pointCommitCheckAt(t, a.worktree, h.srv.URL)
	out, err := stageAndCommit(t, a.worktree, map[string]string{"internal/domain/zone.go": "package domain\n"})
	if err == nil || !strings.Contains(out, "Architecture rule stay-in-your-layer (blocking)") {
		t.Errorf("a commit that breaks a blocking rule = %v, %s; want it refused", err, out)
	}
}

func TestBootRepairsASessionTheServerDoesNotRunItself(t *testing.T) {
	h := aRestartedHarness(t)
	sess, _, err := h.orch.StartInteractive(h.ctx, ports.InteractiveRequest{ProjectID: h.project, RepositoryID: h.repo, TicketID: h.task, Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	worktree := sess.WorkingDir
	if out, err := gitIn(worktree, "config", "--worktree", "core.hooksPath", "/nonexistent/harness-githooks-2"); err != nil {
		t.Fatalf("%v %s", err, out)
	}

	h.restart()

	if got, want := hooksPathOf(t, worktree), filepath.Join(h.dataDir, "githooks", sess.ID); got != want {
		t.Fatalf("core.hooksPath of a terminal client session = %q, want %q", got, want)
	}
}

func TestBootRepairsTheOthersWhenOneSessionsRepairFails(t *testing.T) {
	h := aRestartedHarness(t)
	broken := h.as("go-developer")
	fine := h.as("software-architect")
	for _, a := range []runningSession{broken, fine} {
		if out, err := gitIn(a.worktree, "config", "--worktree", "core.hooksPath", "/nonexistent/old"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	// A file where the folder belongs makes this session's repair fail.
	githooks := filepath.Join(h.dataDir, "githooks", broken.id)
	if err := os.RemoveAll(githooks); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(githooks, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.restart()

	if got, want := hooksPathOf(t, fine.worktree), filepath.Join(h.dataDir, "githooks", fine.id); got != want {
		t.Errorf("the other session's core.hooksPath = %q, want %q", got, want)
	}
}

// commitCheckNotes are the inbox notifications about a session's commit check.
func (h *archHooks) commitCheckNotes(sessionID string) []*domain.Notification {
	h.t.Helper()
	page, err := h.notes.ListNotifications(h.ctx, ports.NotificationQuery{Limit: 200})
	if err != nil {
		h.t.Fatal(err)
	}
	var out []*domain.Notification
	for _, n := range page.Notifications {
		if n.Kind == domain.NotificationCommitCheckOff && n.SessionID == sessionID {
			out = append(out, n)
		}
	}
	return out
}

// breakRepair leaves the session's worktree on a folder that is gone and puts
// a file where its hooks dir belongs, so the repair fails.
func breakRepair(t *testing.T, h *archHooks, a runningSession) string {
	t.Helper()
	if out, err := gitIn(a.worktree, "config", "--worktree", "core.hooksPath", "/nonexistent/harness-githooks-3"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	hooks := filepath.Join(h.dataDir, "githooks", a.id)
	if err := os.RemoveAll(hooks); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	return hooks
}

func TestAFailedRepairMarksTheSessionOffTellsThePersonOnceAndClearsWhenRepaired(t *testing.T) {
	h := aRestartedHarness(t)
	a := h.as("go-developer")
	hooks := breakRepair(t, h, a)

	h.restart()
	sess, err := h.orch.Get(h.ctx, a.id)
	if err != nil || !strings.Contains(sess.CommitCheckOff, "the repair failed") {
		t.Fatalf("commit check off = %q (%v), want the repair failure as the reason", sessCommitOff(sess), err)
	}
	notes := h.commitCheckNotes(a.id)
	if len(notes) != 1 || notes[0].Level != domain.NotificationAction || notes[0].ResolvedAt != nil || !strings.Contains(notes[0].Body, "the repair failed") {
		t.Fatalf("notifications after the boot = %d, first %+v, want one open action notification with the reason", len(notes), *notes[0])
	}

	h.restart() // still broken: no second notification
	if notes := h.commitCheckNotes(a.id); len(notes) != 1 {
		t.Errorf("%d notifications after a second boot while still off, want 1", len(notes))
	}

	if err := os.Remove(hooks); err != nil {
		t.Fatal(err)
	}
	h.restart()
	if sess, _ := h.orch.Get(h.ctx, a.id); sessCommitOff(sess) != "" {
		t.Errorf("commit check off = %q after a successful repair, want it cleared", sess.CommitCheckOff)
	}
	if notes := h.commitCheckNotes(a.id); len(notes) != 1 || notes[0].ResolvedAt == nil {
		t.Errorf("notifications after the repair = %+v, want the one resolved", notes)
	}
}

func sessCommitOff(s *domain.Session) string {
	if s == nil {
		return ""
	}
	return s.CommitCheckOff
}

func TestAMissingHooksFolderIsReportedWithItsReasonAndRepairedAtBoot(t *testing.T) {
	h := aRestartedHarness(t)
	a := h.as("go-developer")
	gone := filepath.Join(t.TempDir(), "harness-githooks-4")
	if out, err := gitIn(a.worktree, "config", "--worktree", "core.hooksPath", gone); err != nil {
		t.Fatalf("%v %s", err, out)
	}

	h.restart()

	notes := h.commitCheckNotes(a.id)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "is missing") || !strings.Contains(notes[0].Body, gone) {
		t.Fatalf("notifications = %+v, want one that names the missing folder %s", notes, gone)
	}
	if notes[0].ResolvedAt == nil {
		t.Error("the notification is still open after the boot repaired the worktree")
	}
	if sess, _ := h.orch.Get(h.ctx, a.id); sessCommitOff(sess) != "" {
		t.Errorf("commit check off = %q after the repair", sess.CommitCheckOff)
	}
}

func TestAResumeTellsTheAgentThatTheCommitCheckIsOffAndNotToBypassHooks(t *testing.T) {
	h := aRestartedHarness(t)
	h.orch.Transcripts = transcriptsThatResume{}
	a := h.as("go-developer")
	if _, err := h.orch.EndInteractive(h.ctx, a.id, 0, false); err != nil {
		t.Fatal(err)
	}
	breakRepair(t, h, a)

	_, spec, err := h.orch.ResumeInteractive(h.ctx, ports.ResumeRequest{SessionID: a.id})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Commit check off", "Do not bypass hooks", "the repair failed"} {
		if !strings.Contains(spec.AppendSystem, want) {
			t.Errorf("the resumed agent's instructions lack %q", want)
		}
	}
}
