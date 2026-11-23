package orchestration

import (
	"context"
	"os"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TaskWorktreeLister = (*Service)(nil)

// taskSessionsIn is the sessions of a task in a repository that have a
// worktree on record, oldest first: the one query the architect's read hook
// and the New session form's picker share, so they cannot disagree on what a
// task's worktree is.
func (s *Service) taskSessionsIn(ctx context.Context, ticketID, repositoryID string) []*domain.Session {
	var out []*domain.Session
	for _, o := range s.sessions.List(ctx, ports.SessionFilter{TicketID: ticketID}) {
		if o.RepositoryID == repositoryID && o.WorkingDir != "" && o.WorktreeClearedAt == nil {
			out = append(out, o)
		}
	}
	slices.SortStableFunc(out, func(a, b *domain.Session) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

// TaskWorktreeChoices implements ports.TaskWorktreeLister: the worktrees of
// the task's sessions in the repository that are still on disk, newest first,
// each with the sessions on it. A session from before shared worktrees is
// listed with write access. The policy block holds the no-policy values until
// the per-task worktree setting exists.
func (s *Service) TaskWorktreeChoices(ctx context.Context, taskID, repositoryID string) (*ports.TaskWorktreeChoices, error) {
	if taskID == "" || repositoryID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "task_id and repository_id are required"}
	}
	out := &ports.TaskWorktreeChoices{
		Policy:    ports.TaskWorktreePolicy{TaskOverride: "project"},
		Worktrees: []ports.TaskWorktreeChoice{},
	}
	lookup, _ := s.workspaces.(worktreeLookup)
	if lookup == nil {
		return out, nil
	}
	byWorktree, cut := map[string]*ports.TaskWorktreeChoice{}, map[string]bool{}
	var order []string // the worktrees by their oldest session
	for _, o := range s.taskSessionsIn(ctx, taskID, repositoryID) {
		if o.WorkspaceID == "" {
			continue
		}
		w, known := byWorktree[o.WorkspaceID]
		if !known {
			if ws := lookup.Get(ctx, o.WorkspaceID); ws != nil {
				if _, err := os.Stat(ws.Path); err == nil {
					w = &ports.TaskWorktreeChoice{WorktreeID: ws.ID, Branch: ws.Branch, OnDisk: true, CreatedBySessionID: o.ID, Sessions: []ports.TaskWorktreeSession{}}
					order = append(order, o.WorkspaceID)
				}
			}
			byWorktree[o.WorkspaceID] = w
		}
		if w == nil {
			continue
		}
		// The creator is the session that cut the worktree; with it gone, the
		// oldest session left stands in.
		if !o.Joined && !cut[o.WorkspaceID] {
			w.CreatedBySessionID, cut[o.WorkspaceID] = o.ID, true
		}
		access := string(o.Access)
		if access == "" {
			access = string(domain.AccessWrite)
		}
		w.Sessions = append(w.Sessions, ports.TaskWorktreeSession{
			SessionID: o.ID, Agent: s.agentName(ctx, o.AgentID), Mode: string(o.Mode), Access: access, Running: !o.Status.IsTerminal(),
		})
	}
	slices.Reverse(order)
	for _, id := range order {
		out.Worktrees = append(out.Worktrees, *byWorktree[id])
	}
	return out, nil
}
