package workspaces

import (
	"context"
	"time"

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
		if err := s.clearOne(ctx, f.session); err != nil {
			skip.Reason, skip.Message = "failed", err.Error()
			res.Skipped = append(res.Skipped, skip)
			continue
		}
		res.Cleared++
	}
	return res, nil
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
