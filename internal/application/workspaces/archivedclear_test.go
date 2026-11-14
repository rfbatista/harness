package workspaces_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type announced struct {
	mu  sync.Mutex
	ids []string
}

func (a *announced) AnnounceSession(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ids = append(a.ids, id)
}

// failingWorktrees refuses to remove one path and otherwise removes for real.
type failingWorktrees struct {
	ports.WorktreeManager
	path string
}

func (f failingWorktrees) Remove(root, path string) error {
	if path == f.path {
		return errors.New("device busy")
	}
	return f.WorktreeManager.Remove(root, path)
}

func (f *mergeFixture) archivedWorktrees(wt ports.WorktreeManager) (*workspaces.ArchivedWorktrees, *announced) {
	f.t.Helper()
	repos := sqlite.NewRepositoryRepository(f.db)
	ws := sqlite.NewWorkspaceRepository(f.db)
	if wt == nil {
		wt = gitcli.NewWorktreeManager()
	}
	ann := &announced{}
	svc := workspaces.NewService(ws, repos, wt, nil)
	return workspaces.NewArchivedWorktrees(workspaces.ArchivedWorktreesDeps{
		Projects: sqlite.NewProjectRepository(f.db), Tickets: sqlite.NewTicketRepository(f.db),
		Sessions: sqlite.NewSessionRepository(f.db), Workspaces: ws, Remover: svc, Status: gitcli.NewCode(), Announce: ann,
	}), ann
}

func (f *mergeFixture) archive(taskID string) {
	f.t.Helper()
	if _, err := f.plan.ArchiveTicket(f.ctx, taskID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *mergeFixture) stop(branch string, status domain.SessionStatus) {
	f.t.Helper()
	if err := sqlite.NewSessionRepository(f.db).UpdateStatus(f.sessIDs[branch], status); err != nil {
		f.t.Fatal(err)
	}
}

func (f *mergeFixture) gone(branch string) bool {
	_, err := os.Stat(f.worktrees[branch])
	return os.IsNotExist(err)
}

func (f *mergeFixture) hasBranch(branch string) bool {
	return gitOut(f.t, f.repoDir, "branch", "--list", branch) != ""
}

func TestClearArchivedWorktreesRemovesFoldersAndKeepsBranchesAndSessions(t *testing.T) {
	f := newMergeFixture(t, "main")
	a, b := f.task(domain.TicketStatusDone), f.task(domain.TicketStatusDone)
	f.session(a, "br-a", "main", 1)
	f.session(b, "br-b", "main", 1)
	f.archive(a)
	f.archive(b)
	svc, ann := f.archivedWorktrees(nil)

	res, err := svc.Clear(f.ctx, ports.ArchivedWorktreesScope{ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cleared != 2 || len(res.Skipped) != 0 {
		t.Fatalf("result = %+v", res)
	}
	sessions := sqlite.NewSessionRepository(f.db)
	for _, br := range []string{"br-a", "br-b"} {
		if !f.gone(br) || !f.hasBranch(br) {
			t.Errorf("%s: folder gone=%v branch kept=%v", br, f.gone(br), f.hasBranch(br))
		}
		s := sessions.Get(f.sessIDs[br])
		if s == nil || s.WorktreeClearedAt == nil || s.WorkspaceID != "" || s.Branch != br {
			t.Errorf("%s: session %+v", br, s)
		}
		if ws := sqlite.NewWorkspaceRepository(f.db).Get(f.ctx, f.wsIDs[br]); ws != nil {
			t.Errorf("%s: workspace row still on record", br)
		}
	}
	if len(ann.ids) != 2 {
		t.Errorf("announced %v, want both sessions", ann.ids)
	}

	again, err := svc.Clear(f.ctx, ports.ArchivedWorktreesScope{ProjectID: f.projectID})
	if err != nil || again.Cleared != 0 || len(again.Skipped) != 0 {
		t.Fatalf("second clear = %+v, %v", again, err)
	}
}

func TestClearArchivedWorktreesOfOneTaskLeavesTheOthers(t *testing.T) {
	f := newMergeFixture(t, "main")
	a, b := f.task(domain.TicketStatusDone), f.task(domain.TicketStatusDone)
	f.session(a, "br-a", "main", 0)
	f.session(b, "br-b", "main", 0)
	f.archive(a)
	f.archive(b)
	svc, _ := f.archivedWorktrees(nil)

	res, err := svc.Clear(f.ctx, ports.ArchivedWorktreesScope{TaskID: a})
	if err != nil || res.Cleared != 1 {
		t.Fatalf("clear = %+v, %v", res, err)
	}
	if !f.gone("br-a") || f.gone("br-b") {
		t.Fatalf("a gone=%v b gone=%v", f.gone("br-a"), f.gone("br-b"))
	}
}

func TestClearArchivedWorktreesSkipsRunningAndReportsFailuresWithoutStopping(t *testing.T) {
	f := newMergeFixture(t, "main")
	a := f.task(domain.TicketStatusDone)
	f.session(a, "br-running", "main", 0)
	f.session(a, "br-stuck", "main", 0)
	f.session(a, "br-fine", "main", 0)
	f.archive(a)
	f.stop("br-running", domain.SessionRunning)
	svc, _ := f.archivedWorktrees(failingWorktrees{WorktreeManager: gitcli.NewWorktreeManager(), path: f.worktrees["br-stuck"]})

	res, err := svc.Clear(f.ctx, ports.ArchivedWorktreesScope{TaskID: a})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cleared != 1 || len(res.Skipped) != 2 {
		t.Fatalf("result = %+v", res)
	}
	reasons := map[string]ports.ClearSkip{}
	for _, s := range res.Skipped {
		reasons[s.SessionID] = s
	}
	if r := reasons[f.sessIDs["br-running"]]; r.Reason != "running" || r.TaskID != a || r.TaskTitle != "task" {
		t.Errorf("running skip = %+v", r)
	}
	if r := reasons[f.sessIDs["br-stuck"]]; r.Reason != "failed" || r.Message == "" {
		t.Errorf("failed skip = %+v", r)
	}
	if f.gone("br-running") || f.gone("br-stuck") || !f.gone("br-fine") {
		t.Error("only the fine worktree should be gone")
	}
	if s := sqlite.NewSessionRepository(f.db).Get(f.sessIDs["br-stuck"]); s.WorktreeClearedAt != nil || s.WorkspaceID == "" {
		t.Errorf("failed session lost its worktree record: %+v", s)
	}
}

func TestClearArchivedWorktreesOfAFolderAlreadyMissing(t *testing.T) {
	f := newMergeFixture(t, "main")
	a := f.task(domain.TicketStatusDone)
	f.session(a, "br-a", "main", 0)
	f.archive(a)
	if err := os.RemoveAll(f.worktrees["br-a"]); err != nil {
		t.Fatal(err)
	}
	svc, _ := f.archivedWorktrees(nil)
	res, err := svc.Clear(f.ctx, ports.ArchivedWorktreesScope{TaskID: a})
	if err != nil || res.Cleared != 1 || len(res.Skipped) != 0 {
		t.Fatalf("clear = %+v, %v", res, err)
	}
}

func TestClearArchivedWorktreesIncludesAFeaturesChildren(t *testing.T) {
	f := newMergeFixture(t, "main")
	feature := f.task(domain.TicketStatusDone)
	child, err := f.plan.CreateTicket(f.ctx, f.projectID, "child", "", domain.TicketStatusDone, feature)
	if err != nil {
		t.Fatal(err)
	}
	f.session(child.ID, "br-child", "main", 0)
	f.archive(feature)
	svc, _ := f.archivedWorktrees(nil)
	res, err := svc.Clear(f.ctx, ports.ArchivedWorktreesScope{TaskID: feature})
	if err != nil || res.Cleared != 1 || !f.gone("br-child") {
		t.Fatalf("clear = %+v, %v", res, err)
	}
}

func TestClearArchivedWorktreesRefusals(t *testing.T) {
	f := newMergeFixture(t, "main")
	open := f.task(domain.TicketStatusDone)
	f.session(open, "br-open", "main", 0)
	svc, _ := f.archivedWorktrees(nil)

	for _, c := range []struct {
		scope ports.ArchivedWorktreesScope
		code  string
	}{
		{ports.ArchivedWorktreesScope{TaskID: open}, "TASK_NOT_ARCHIVED"},
		{ports.ArchivedWorktreesScope{TaskID: "nope"}, "TICKET_NOT_FOUND"},
		{ports.ArchivedWorktreesScope{ProjectID: "nope"}, "PROJECT_NOT_FOUND"},
		{ports.ArchivedWorktreesScope{}, "INVALID_INPUT"},
	} {
		if _, err := svc.Clear(f.ctx, c.scope); errs.Code(err) != c.code {
			t.Errorf("%+v: err = %v, want %s", c.scope, err, c.code)
		}
	}
	if f.gone("br-open") {
		t.Error("an unarchived task's worktree was removed")
	}
}

func TestPreviewCountsWhatAClearWouldRemoveLoseAndSkip(t *testing.T) {
	f := newMergeFixture(t, "main")
	a, b := f.task(domain.TicketStatusDone), f.task(domain.TicketStatusDone)
	f.session(a, "clean", "main", 0)
	f.session(a, "modified", "main", 1)
	f.session(a, "untracked", "main", 0)
	f.session(a, "missing", "main", 0)
	f.session(a, "running", "main", 0)
	f.session(b, "other", "main", 0)
	f.archive(a)
	f.archive(b)
	f.stop("running", domain.SessionRunning)
	if err := os.WriteFile(filepath.Join(f.worktrees["modified"], "modified.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.worktrees["untracked"], "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.worktrees["missing"]); err != nil {
		t.Fatal(err)
	}
	svc, _ := f.archivedWorktrees(nil)

	got, err := svc.Preview(f.ctx, ports.ArchivedWorktreesScope{TaskID: a})
	if err != nil || *got != (ports.ArchivedWorktreesPreview{Worktrees: 4, Dirty: 2, Running: 1}) {
		t.Fatalf("task preview = %+v, %v", got, err)
	}
	got, err = svc.Preview(f.ctx, ports.ArchivedWorktreesScope{ProjectID: f.projectID})
	if err != nil || *got != (ports.ArchivedWorktreesPreview{Worktrees: 5, Dirty: 2, Running: 1}) {
		t.Fatalf("project preview = %+v, %v", got, err)
	}
	if f.gone("clean") || !f.hasBranch("modified") {
		t.Error("a preview changed something")
	}

	if _, err := svc.Clear(f.ctx, ports.ArchivedWorktreesScope{ProjectID: f.projectID}); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Preview(f.ctx, ports.ArchivedWorktreesScope{ProjectID: f.projectID})
	if err != nil || *got != (ports.ArchivedWorktreesPreview{Running: 1}) {
		t.Fatalf("preview after a clear = %+v, %v", got, err)
	}
}

func TestPreviewOfAnUnarchivedTaskIsRefused(t *testing.T) {
	f := newMergeFixture(t, "main")
	open := f.task(domain.TicketStatusDone)
	svc, _ := f.archivedWorktrees(nil)
	if _, err := svc.Preview(f.ctx, ports.ArchivedWorktreesScope{TaskID: open}); errs.Code(err) != "TASK_NOT_ARCHIVED" {
		t.Fatalf("err = %v", err)
	}
}
