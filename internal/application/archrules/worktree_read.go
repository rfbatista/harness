package archrules

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.WorktreeReadRecorder = (*Service)(nil)

// WorktreeReadHook names the architect's worktree read hook in a rule event
// (ADR 0087); a denial is always blocked.
const WorktreeReadHook = "worktree_read"

// RecordWorktreeRead implements ports.WorktreeReadRecorder: a denial of the
// worktree read hook is stored with the session and put on the project's feed
// the way a failed rule is, so it shows where rule outcomes show.
func (s *Service) RecordWorktreeRead(ctx context.Context, sessionID, tool, path, reason string) error {
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil {
		return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no such session"}
	}
	agent := domain.PlainAgent
	if sess.AgentID != "" {
		if a, err := s.d.Agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil && a.Name != "" {
			agent = a.Name
		}
	}
	ev := &domain.ArchitectureRuleEvent{
		At: s.d.Now().UTC(), SessionID: sess.ID, TaskID: sess.TicketID, Agent: agent,
		RuleID: WorktreeReadHook, Kind: WorktreeReadHook, AtPoint: domain.PointBeforeTool, Mode: domain.ModeBlocking,
		Outcome: domain.OutcomeBlocked, Path: path, Tool: tool, Detail: reason,
		RepositoryID: sess.RepositoryID, ProjectID: sess.ProjectID,
	}
	if err := s.d.Events.Add(ctx, ev); err != nil {
		return err
	}
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{ArchitectureRuleEvent: &ports.ArchitectureRuleEventChange{
			RepositoryID: ev.RepositoryID, Event: ev, ProjectID: ev.ProjectID,
		}})
	}
	return nil
}
