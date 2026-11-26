package archrules

import (
	"context"
	"slices"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ProtectedPathRule names a protected-path refusal in a rule event (ADR 0012).
const ProtectedPathRule = "protected_path"

// protectedPathRefusal denies a write of a gated session to a path its kept
// configuration protects, made with a file tool, a PATH-shimmed command or a
// redirect in a Bash command. No rule of any record is consulted, and a read or
// an edit that already landed is not refused. A session that is not gated, or
// kept no configuration, may write them. The refusal is recorded with the
// session and put on the project's feed.
func (s *Service) protectedPathRefusal(ctx context.Context, c ports.ArchitectureCheck) (ports.ArchitectureVerdict, bool) {
	if s.d.Guard == nil || c.Point != domain.PointBeforeTool || c.Person {
		return ports.ArchitectureVerdict{}, false
	}
	sess := s.d.Sessions.Get(c.SessionID)
	if sess == nil || sess.WorkingDir == "" {
		return ports.ArchitectureVerdict{}, false
	}
	cfg, ok, err := s.d.Guard.Config(ctx, sess.ID)
	if err != nil || !ok || len(cfg.ProtectedPaths) == 0 {
		return ports.ArchitectureVerdict{}, false
	}
	// A session whose mode is gated keeps the protection from its first write:
	// a mode-less session counts as gated here, as its first write is the diff.
	if !domain.SessionGated(cfg.Mode, true) {
		return ports.ArchitectureVerdict{}, false
	}
	for _, p := range slices.Concat(inside(sess.WorkingDir, c.Paths), inside(sess.WorkingDir, c.ShellWrites)) {
		if glob, hit := domain.ProtectedGlob(cfg.ProtectedPaths, p); hit && !scoped(cfg.Scope, p) {
			s.recordProtected(ctx, sess, c, p, glob)
			return ports.ArchitectureVerdict{Deny: domain.ProtectedPathMessage(p, glob)}, true
		}
	}
	return ports.ArchitectureVerdict{}, false
}

// scoped says whether the session's scope unlocks path: a glob the architect
// gave this session alone.
func scoped(scope []string, path string) bool {
	_, ok := domain.ProtectedGlob(scope, path)
	return ok
}

func (s *Service) recordProtected(ctx context.Context, sess *domain.Session, c ports.ArchitectureCheck, path, glob string) {
	agent := domain.PlainAgent
	if sess.AgentID != "" {
		if a, err := s.d.Agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil && a.Name != "" {
			agent = a.Name
		}
	}
	ev := &domain.ArchitectureRuleEvent{
		At: s.d.Now().UTC(), SessionID: sess.ID, TaskID: sess.TicketID, Agent: agent,
		RuleID: ProtectedPathRule, Kind: ProtectedPathRule, AtPoint: c.Point, Mode: domain.ModeBlocking,
		Outcome: domain.OutcomeBlocked, Path: path, Tool: c.Tool, Detail: strings.TrimSpace("protected glob " + glob),
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
