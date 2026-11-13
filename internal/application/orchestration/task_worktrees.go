package orchestration

import (
	"context"
	"path/filepath"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TaskWorktrees = (*Service)(nil)

// TaskWorktrees implements ports.TaskWorktrees: every session of the caller's
// task in its repository, running or ended, whose directory is known, newest
// first. One store query; no git.
func (s *Service) TaskWorktrees(ctx context.Context, sessionID string) (*ports.TaskWorktreeList, error) {
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
	for _, other := range s.sessions.List(ctx, ports.SessionFilter{TicketID: sess.TicketID}) {
		if other.RepositoryID != sess.RepositoryID || other.WorkingDir == "" {
			continue
		}
		out.Worktrees = append(out.Worktrees, ports.TaskWorktree{
			SessionID: other.ID, Dir: other.WorkingDir, Branch: other.Branch,
			Agent: s.agentName(ctx, other.AgentID), Status: string(other.Status),
		})
	}
	return out, nil
}
