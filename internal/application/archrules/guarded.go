package archrules

import (
	"context"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// guarded is a gated session's check of a write: its session, the guard
// configuration it kept, and the paths of the worktree the action writes.
type guarded struct {
	sess  *domain.Session
	cfg   domain.GuardConfig
	paths []string
}

// guardedWrite answers the session behind a check when the Guard layer applies
// to it: the check is before a tool (or, for atPoint after-edit rules, after an
// edit), is not the person's, and the session kept a configuration of a gated
// mode. A mode-less session counts as gated here: its first write is the diff.
func (s *Service) guardedWrite(ctx context.Context, c ports.ArchitectureCheck, points ...string) (guarded, bool) {
	if s.d.Guard == nil || c.Person || !slices.Contains(points, c.Point) {
		return guarded{}, false
	}
	sess := s.d.Sessions.Get(c.SessionID)
	if sess == nil || sess.WorkingDir == "" {
		return guarded{}, false
	}
	cfg, ok, err := s.d.Guard.Config(ctx, sess.ID)
	if err != nil || !ok || !domain.SessionGated(cfg.Mode, true) {
		return guarded{}, false
	}
	return guarded{sess: sess, cfg: cfg,
		paths: slices.Concat(inside(sess.WorkingDir, c.Paths), inside(sess.WorkingDir, c.ShellWrites))}, true
}

// ruleEvent is a refusal or flag of a Guard rule, as it is recorded.
type ruleEvent struct {
	rule, mode, outcome, path, detail string
}

// recordRule stores a Guard rule's event with the session and puts it on the
// project's feed, as a failed architecture rule is.
func (s *Service) recordRule(ctx context.Context, sess *domain.Session, c ports.ArchitectureCheck, e ruleEvent) {
	agent := domain.PlainAgent
	if sess.AgentID != "" {
		if a, err := s.d.Agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil && a.Name != "" {
			agent = a.Name
		}
	}
	ev := &domain.ArchitectureRuleEvent{
		At: s.d.Now().UTC(), SessionID: sess.ID, TaskID: sess.TicketID, Agent: agent,
		RuleID: e.rule, Kind: e.rule, AtPoint: c.Point, Mode: e.mode, Outcome: e.outcome, Path: e.path, Tool: c.Tool, Detail: e.detail,
		RepositoryID: sess.RepositoryID, ProjectID: sess.ProjectID,
	}
	if s.d.Events.Add(ctx, ev) != nil {
		return
	}
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{ArchitectureRuleEvent: &ports.ArchitectureRuleEventChange{
			RepositoryID: ev.RepositoryID, Event: ev, ProjectID: ev.ProjectID,
		}})
	}
}
