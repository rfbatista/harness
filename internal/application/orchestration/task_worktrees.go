package orchestration

import (
	"context"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TaskWorktrees = (*Service)(nil)

// TaskWorktrees implements ports.TaskWorktrees: every session of the caller's
// task in its repository, running or ended, whose directory is known, newest
// first. One store query; no git.
func (s *Service) TaskWorktrees(ctx context.Context, sessionID, path string) (*ports.TaskWorktreeList, error) {
	sess := s.sessions.Get(sessionID)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no such session"}
	}
	out := &ports.TaskWorktreeList{Worktrees: []ports.TaskWorktree{}}
	if s.Sandbox != nil && s.Sandbox.WorkspacesRoot != nil && sess.RepositoryID != "" {
		if repo, err := s.catalog.Repositories.GetRepository(ctx, sess.RepositoryID); err == nil && repo != nil {
			out.Root = filepath.Join(s.Sandbox.WorkspacesRoot(), domain.RepositoryWorktreesDirName(repo))
		}
	}
	if sess.TicketID == "" {
		return out, nil
	}
	if path != "" {
		out.Owner = s.worktreeOwner(ctx, sess, out.Root, path)
	}
	for _, other := range s.sessions.List(ctx, ports.SessionFilter{TicketID: sess.TicketID}) {
		if other.RepositoryID != sess.RepositoryID || other.WorkingDir == "" || other.WorktreeClearedAt != nil {
			continue
		}
		out.Worktrees = append(out.Worktrees, ports.TaskWorktree{
			SessionID: other.ID, Dir: other.WorkingDir, Branch: other.Branch,
			Agent: s.agentName(ctx, other.AgentID), Status: string(other.Status),
		})
	}
	return out, nil
}

// worktreeOwner is the session and task of the worktree path lies in, when it is
// a session of another task of the project; nil when the path is under no
// session's worktree or the session has no task. It reads the project's
// sessions, so only a denial asks it.
func (s *Service) worktreeOwner(ctx context.Context, caller *domain.Session, root, path string) *ports.TaskWorktreeOwner {
	if root == "" {
		return nil
	}
	rel, ok := strings.CutPrefix(filepath.Clean(path), root+string(filepath.Separator))
	if !ok {
		return nil
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	dir := filepath.Join(root, first)
	for _, other := range s.sessions.List(ctx, ports.SessionFilter{ProjectID: caller.ProjectID}) {
		if filepath.Clean(other.WorkingDir) != dir || other.TicketID == "" || other.TicketID == caller.TicketID {
			continue
		}
		title := ""
		if s.tickets != nil {
			if tk := s.tickets.Get(ctx, other.TicketID); tk != nil {
				title = tk.Title
			}
		}
		return &ports.TaskWorktreeOwner{SessionID: other.ID, TaskID: other.TicketID, TaskTitle: title}
	}
	return nil
}
