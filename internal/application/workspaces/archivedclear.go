package workspaces

import (
	"context"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// WorkspaceRemover removes a workspace's worktree and its row (Service.Delete).
type WorkspaceRemover interface {
	Delete(id string) error
}

// ArchivedWorktreesDeps are what Clear worktrees reads and acts through.
type ArchivedWorktreesDeps struct {
	Projects   ports.ProjectRepository
	Tickets    ports.TicketRepository
	Sessions   ports.SessionRepository
	Workspaces ports.WorkspaceRepository
	// Remover is the removal session deletion uses: it keeps the branch.
	Remover WorkspaceRemover
	// Status reads a worktree's uncommitted changes, for the preview only.
	Status ports.GitWorktreeReader
	// Announce tells open pages a session changed; may be nil.
	Announce ports.SessionChangeAnnouncer
	// Now stamps when a worktree was cleared; nil is time.Now.
	Now func() time.Time
}

// ArchivedWorktrees is the person's Clear worktrees action (ADR 0089).
type ArchivedWorktrees struct{ d ArchivedWorktreesDeps }

var _ ports.ArchivedWorktrees = (*ArchivedWorktrees)(nil)

// NewArchivedWorktrees returns the use case.
func NewArchivedWorktrees(d ArchivedWorktreesDeps) *ArchivedWorktrees {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &ArchivedWorktrees{d: d}
}

// inScope is a session that still has a worktree on record, with its task.
type inScope struct {
	session *domain.Session
	task    *domain.Ticket
}

// scope reads the sessions, with a worktree on record, of the archived tasks
// the scope names.
func (s *ArchivedWorktrees) scope(ctx context.Context, sc ports.ArchivedWorktreesScope) ([]inScope, error) {
	var tasks []*domain.Ticket
	switch {
	case sc.TaskID != "":
		tk := s.d.Tickets.Get(ctx, sc.TaskID)
		if tk == nil {
			return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
		}
		if tk.ArchivedAt == nil {
			return nil, &domain.StructuredError{Code: "TASK_NOT_ARCHIVED", Message: "task is not archived"}
		}
		tasks = append(tasks, tk)
		for _, c := range s.d.Tickets.ListArchivedByProject(ctx, tk.ProjectID) {
			if c.ParentID == tk.ID {
				tasks = append(tasks, c)
			}
		}
	case sc.ProjectID != "":
		if s.d.Projects.Get(ctx, sc.ProjectID) == nil {
			return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
		}
		tasks = s.d.Tickets.ListArchivedByProject(ctx, sc.ProjectID)
	default:
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "task_id or project_id is required"}
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	byID := make(map[string]*domain.Ticket, len(tasks))
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	sessions, err := s.d.Sessions.Query(ctx, ports.SessionFilter{TicketIDs: ids})
	if err != nil {
		return nil, err
	}
	var out []inScope
	for _, sess := range sessions {
		if sess.WorkspaceID != "" {
			out = append(out, inScope{session: sess, task: byID[sess.TicketID]})
		}
	}
	return out, nil
}

// Clear removes the worktrees in scope. A worktree whose workspace row or
// folder is already gone counts as cleared.
func (s *ArchivedWorktrees) Clear(ctx context.Context, sc ports.ArchivedWorktreesScope) (*ports.ClearResult, error) {
	found, err := s.scope(ctx, sc)
	if err != nil {
		return nil, err
	}
	res := &ports.ClearResult{Skipped: []ports.ClearSkip{}}
	for _, f := range found {
		skip := ports.ClearSkip{SessionID: f.session.ID, TaskID: f.task.ID, TaskTitle: f.task.Title}
		if !f.session.Status.IsTerminal() {
			skip.Reason = "running"
			res.Skipped = append(res.Skipped, skip)
			continue
		}
		if reason := s.sharedBlock(ctx, f.session, found); reason != "" {
			skip.Reason = reason
			res.Skipped = append(res.Skipped, skip)
			continue
		}
		if err := s.clearOne(ctx, f.session); err != nil {
			skip.Reason, skip.Message = "failed", err.Error()
			res.Skipped = append(res.Skipped, skip)
			continue
		}
		res.Cleared++
	}
	return res, nil
}

// sharedBlock says why the session's worktree must stay: "running" while any
// other session on it runs, "shared" while a session that is not in the clear
// (another task's) is on it. A worktree no one else is on, the usual case, is
// not blocked. Sessions in the clear that end up here later find the worktree
// already cleared and count as cleared.
func (s *ArchivedWorktrees) sharedBlock(ctx context.Context, sess *domain.Session, found []inScope) string {
	cleared := make(map[string]bool, len(found))
	for _, f := range found {
		cleared[f.session.ID] = true
	}
	others, err := s.d.Sessions.Query(ctx, ports.SessionFilter{WorkspaceID: sess.WorkspaceID})
	if err != nil {
		return ""
	}
	reason := ""
	for _, o := range others {
		switch {
		case o.ID == sess.ID:
		case !o.Status.IsTerminal():
			return "running"
		case !cleared[o.ID]:
			reason = "shared"
		}
	}
	return reason
}

func (s *ArchivedWorktrees) clearOne(ctx context.Context, sess *domain.Session) error {
	if s.d.Workspaces.Get(ctx, sess.WorkspaceID) != nil {
		if err := s.d.Remover.Delete(sess.WorkspaceID); err != nil {
			return err
		}
	}
	if err := s.d.Sessions.UpdateWorktreeCleared(sess.ID, s.d.Now()); err != nil {
		return err
	}
	if s.d.Announce != nil {
		s.d.Announce.AnnounceSession(sess.ID)
	}
	return nil
}

// Preview counts what a clear would remove, how much of it holds uncommitted
// changes, and what it would skip as running. It reads git status per
// worktree, so only the preview and never a page's first paint asks for it.
func (s *ArchivedWorktrees) Preview(ctx context.Context, sc ports.ArchivedWorktreesScope) (*ports.ArchivedWorktreesPreview, error) {
	found, err := s.scope(ctx, sc)
	if err != nil {
		return nil, err
	}
	out := &ports.ArchivedWorktreesPreview{}
	for _, f := range found {
		if !f.session.Status.IsTerminal() {
			out.Running++
			continue
		}
		out.Worktrees++
		dirty, err := s.dirty(ctx, f.session.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if dirty {
			out.Dirty++
		}
	}
	return out, nil
}

// dirty says whether the workspace's worktree holds uncommitted changes. A
// worktree that is missing from disk holds none.
func (s *ArchivedWorktrees) dirty(ctx context.Context, workspaceID string) (bool, error) {
	ws := s.d.Workspaces.Get(ctx, workspaceID)
	if ws == nil {
		return false, nil
	}
	st, err := s.d.Status.WorktreeStatus(ctx, ws.Path)
	if errs.Code(err) == "WORKSPACE_MISSING" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.Tracked+st.Untracked+st.Staged > 0, nil
}
