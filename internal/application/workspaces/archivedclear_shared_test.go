package workspaces_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Clearing the worktrees of archived tasks when sessions share a worktree
// (Glossary: Shared worktree). Only a fake remover and the store: no git.

// recordingRemover stands in for the workspaces service: it drops the
// worktree's row, as the real removal does, and records the call.
type recordingRemover struct {
	rows    *sqlite.WorkspaceRepository
	removed []string
}

func (r *recordingRemover) Delete(id string) error {
	r.removed = append(r.removed, id)
	return r.rows.Delete(id)
}

// cleanStatus reads every worktree as holding no uncommitted change.
type cleanStatus struct{ ports.GitWorktreeReader }

func (cleanStatus) WorktreeStatus(context.Context, string) (*ports.GitWorktreeStatus, error) {
	return &ports.GitWorktreeStatus{}, nil
}

type sharedClearWorld struct {
	t       *testing.T
	ctx     context.Context
	plan    *planning.Service
	project string
	repo    string
	sess    *sqlite.SessionRepository
	ws      *sqlite.WorkspaceRepository
	remover *recordingRemover
	clear   *workspaces.ArchivedWorktrees
	db      *gorm.DB
}

func newSharedClearWorld(t *testing.T) *sharedClearWorld {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.NewRepositoryRepository(db).Create(p.ID, "app", "", "https://example.com/app.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &sharedClearWorld{db: db, t: t, ctx: context.Background(), project: p.ID, repo: repo.ID, sess: sqlite.NewSessionRepository(db), ws: sqlite.NewWorkspaceRepository(db),
		plan: planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)}
	w.remover = &recordingRemover{rows: w.ws}
	w.clear = workspaces.NewArchivedWorktrees(workspaces.ArchivedWorktreesDeps{
		Projects: projects, Tickets: sqlite.NewTicketRepository(db), Sessions: w.sess, Workspaces: w.ws, Remover: w.remover, Status: cleanStatus{},
	})
	return w
}

// task is an archived task.
func (w *sharedClearWorld) task(title string) string {
	w.t.Helper()
	tk, err := w.plan.CreateTicket(w.ctx, w.project, title, "", domain.TicketStatusDone, "")
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.plan.ArchiveTicket(w.ctx, tk.ID); err != nil {
		w.t.Fatal(err)
	}
	return tk.ID
}

func (w *sharedClearWorld) worktree(name string) *domain.Workspace {
	w.t.Helper()
	ws, err := w.ws.Create(domain.Workspace{RepositoryID: w.repo, Name: name, Branch: name, Path: w.t.TempDir()})
	if err != nil {
		w.t.Fatal(err)
	}
	return ws
}

func (w *sharedClearWorld) session(task string, ws *domain.Workspace, status domain.SessionStatus) string {
	w.t.Helper()
	s, err := w.sess.Create(&domain.Session{ProjectID: w.project, TicketID: task, RepositoryID: w.repo, WorkspaceID: ws.ID, Branch: ws.Branch, Status: status})
	if err != nil {
		w.t.Fatal(err)
	}
	return s.ID
}

func TestAClearLeavesASharedWorktreeWhileAnySessionOnItIsRunning(t *testing.T) {
	w := newSharedClearWorld(t)
	task := w.task("shared")
	ws := w.worktree("agent/shared")
	ended := w.session(task, ws, domain.SessionDone)
	running := w.session(task, ws, domain.SessionRunning)

	res, err := w.clear.Clear(w.ctx, ports.ArchivedWorktreesScope{TaskID: task})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.remover.removed) != 0 || res.Cleared != 0 {
		t.Fatalf("removed %v, cleared %d; a worktree with a running session stays", w.remover.removed, res.Cleared)
	}
	reasons := map[string]string{}
	for _, s := range res.Skipped {
		reasons[s.SessionID] = s.Reason
	}
	if reasons[ended] != "running" || reasons[running] != "running" {
		t.Errorf("skipped = %v, want both sessions skipped as running", reasons)
	}
	if got := w.sess.Get(ended); got.WorkspaceID != ws.ID || got.WorktreeClearedAt != nil {
		t.Errorf("the ended session lost its worktree record: %+v", got)
	}

	// Once the last session ended, the worktree is cleared once for both.
	if err := w.sess.UpdateStatus(running, domain.SessionDone); err != nil {
		t.Fatal(err)
	}
	res, err = w.clear.Clear(w.ctx, ports.ArchivedWorktreesScope{TaskID: task})
	if err != nil || res.Cleared != 1 || len(res.Skipped) != 0 {
		t.Fatalf("clear after both ended: %+v, %v; want the worktree counted once", res, err)
	}
	for _, id := range []string{ended, running} {
		if got := w.sess.Get(id); got.WorktreeClearedAt == nil || got.WorkspaceID != "" {
			t.Errorf("session %s after the clear: %+v", id, got)
		}
	}
	if len(w.remover.removed) != 1 || w.remover.removed[0] != ws.ID {
		t.Errorf("removed %v, want the shared worktree once", w.remover.removed)
	}
}

func TestAClearLeavesASharedWorktreeWhenASessionOfATaskNotBeingClearedIsOnIt(t *testing.T) {
	w := newSharedClearWorld(t)
	archived := w.task("archived")
	kept, err := w.plan.CreateTicket(w.ctx, w.project, "kept", "", domain.TicketStatusDone, "")
	if err != nil {
		t.Fatal(err)
	}
	ws := w.worktree("agent/mixed")
	mine := w.session(archived, ws, domain.SessionDone)
	w.session(kept.ID, ws, domain.SessionDone)

	res, err := w.clear.Clear(w.ctx, ports.ArchivedWorktreesScope{TaskID: archived})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.remover.removed) != 0 || res.Cleared != 0 {
		t.Fatalf("removed %v, cleared %d; a session of another task is still on it", w.remover.removed, res.Cleared)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].SessionID != mine || res.Skipped[0].Reason != "shared" {
		t.Errorf("skipped = %+v, want the archived session skipped as shared", res.Skipped)
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Errorf("the worktree is gone: %v", err)
	}
}

func TestAClearOfAWorktreeNoOtherSessionIsOnWorksAsBefore(t *testing.T) {
	w := newSharedClearWorld(t)
	task := w.task("alone")
	ws := w.worktree("agent/alone")
	id := w.session(task, ws, domain.SessionDone)
	res, err := w.clear.Clear(w.ctx, ports.ArchivedWorktreesScope{TaskID: task})
	if err != nil || res.Cleared != 1 || len(res.Skipped) != 0 || len(w.remover.removed) != 1 {
		t.Fatalf("%+v %v removed %v", res, err, w.remover.removed)
	}
	if got := w.sess.Get(id); got.WorktreeClearedAt == nil || got.WorkspaceID != "" {
		t.Errorf("session after the clear: %+v", got)
	}
}

// failingQuery fails the per-worktree session query the guard makes.
type failingQuery struct {
	ports.SessionRepository
}

func (f failingQuery) Query(ctx context.Context, filter ports.SessionFilter) ([]*domain.Session, error) {
	if filter.WorkspaceID != "" {
		return nil, errors.New("the store is down")
	}
	return f.SessionRepository.Query(ctx, filter)
}

func TestAClearThatCannotTellWhoIsOnAWorktreeLeavesItAndSaysSo(t *testing.T) {
	w := newSharedClearWorld(t)
	task := w.task("unsure")
	ws := w.worktree("agent/unsure")
	id := w.session(task, ws, domain.SessionDone)
	clear := workspaces.NewArchivedWorktrees(workspaces.ArchivedWorktreesDeps{
		Projects: sqlite.NewProjectRepository(w.db), Tickets: sqlite.NewTicketRepository(w.db),
		Sessions: failingQuery{w.sess}, Workspaces: w.ws, Remover: w.remover,
	})

	res, err := clear.Clear(w.ctx, ports.ArchivedWorktreesScope{TaskID: task})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.remover.removed) != 0 || res.Cleared != 0 {
		t.Fatalf("removed %v, cleared %d; an unanswered question never removes", w.remover.removed, res.Cleared)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].SessionID != id || res.Skipped[0].Reason != "error" || res.Skipped[0].Message == "" {
		t.Errorf("skipped = %+v, want the session skipped as error, with the reason", res.Skipped)
	}
}

func TestThePreviewDecidesAsTheClearDoesAndCountsEachWorktreeOnce(t *testing.T) {
	w := newSharedClearWorld(t)
	task := w.task("preview")
	other, err := w.plan.CreateTicket(w.ctx, w.project, "other task", "", domain.TicketStatusDone, "")
	if err != nil {
		t.Fatal(err)
	}
	// One worktree two ended sessions of the clear share: one removal.
	both := w.worktree("agent/both")
	w.session(task, both, domain.SessionDone)
	w.session(task, both, domain.SessionDone)
	// One a running session of the clear is on, and one a task outside the clear is on.
	busy := w.worktree("agent/busy")
	w.session(task, busy, domain.SessionDone)
	w.session(task, busy, domain.SessionRunning)
	mixed := w.worktree("agent/mixed")
	w.session(task, mixed, domain.SessionDone)
	w.session(other.ID, mixed, domain.SessionDone)

	scope := ports.ArchivedWorktreesScope{TaskID: task}
	preview, err := w.clear.Preview(w.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Worktrees != 1 || preview.Running != 2 || preview.Shared != 1 {
		t.Fatalf("preview = %+v, want 1 worktree to remove, 2 sessions skipped as running (the running one and the ended one on its worktree), 1 as shared", preview)
	}

	res, err := w.clear.Clear(w.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if res.Cleared != preview.Worktrees || len(w.remover.removed) != preview.Worktrees {
		t.Errorf("the clear removed %d (cleared %d), the preview said %d", len(w.remover.removed), res.Cleared, preview.Worktrees)
	}
	reasons := map[string]int{}
	for _, s := range res.Skipped {
		reasons[s.Reason]++
	}
	if reasons["running"] != preview.Running || reasons["shared"] != preview.Shared {
		t.Errorf("the clear skipped %v, the preview said %d running and %d shared", reasons, preview.Running, preview.Shared)
	}
}
