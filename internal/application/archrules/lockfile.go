package archrules

import (
	"context"
	"slices"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Names of the lockfile rule in a rule event (ADR 0012).
const (
	LockfileRefusalRule = "lockfile_without_manifest"
	LockfileFlagRule    = "lockfile_flag"
)

// lockfileRule applies the lockfile rule of a gated session's kept
// configuration. Before a write, a lockfile whose manifest has not changed in
// the worktree is refused with LOCKFILE_WITHOUT_MANIFEST. After an edit landed,
// a lockfile changed together with its manifest is flagged with the
// dependencies it added. refused is true for the refusal alone.
func (s *Service) lockfileRule(ctx context.Context, c ports.ArchitectureCheck) (ports.ArchitectureVerdict, bool) {
	if s.d.Guard == nil || c.Person || (c.Point != domain.PointBeforeTool && c.Point != domain.PointAfterEdit) {
		return ports.ArchitectureVerdict{}, false
	}
	sess := s.d.Sessions.Get(c.SessionID)
	if sess == nil || sess.WorkingDir == "" {
		return ports.ArchitectureVerdict{}, false
	}
	cfg, ok, err := s.d.Guard.Config(ctx, sess.ID)
	if err != nil || !ok || !cfg.LockfileRule || !domain.SessionGated(cfg.Mode, true) {
		return ports.ArchitectureVerdict{}, false
	}
	for _, rel := range slices.Concat(inside(sess.WorkingDir, c.Paths), inside(sess.WorkingDir, c.ShellWrites)) {
		manifest, isLock := domain.LockfileManifest(rel)
		if !isLock {
			continue
		}
		changed := s.d.ManifestChanged != nil && s.d.ManifestChanged(ctx, sess, manifest)
		switch {
		case c.Point == domain.PointBeforeTool && !changed:
			s.recordLockfile(ctx, sess, c, LockfileRefusalRule, domain.ModeBlocking, domain.OutcomeBlocked, rel, "manifest "+manifest+" unchanged")
			return ports.ArchitectureVerdict{Deny: "LOCKFILE_WITHOUT_MANIFEST: " + rel + " changes without its manifest " + manifest +
				". Change the manifest first (add or update the dependency there), then let the lockfile follow it."}, true
		case c.Point == domain.PointAfterEdit && changed:
			s.flagLockfile(ctx, sess, c, rel, manifest)
		}
	}
	return ports.ArchitectureVerdict{}, false
}

// flagLockfile announces a lockfile changed with its manifest, naming the
// dependencies it added, and records it with the session.
func (s *Service) flagLockfile(ctx context.Context, sess *domain.Session, c ports.ArchitectureCheck, rel, manifest string) {
	if s.d.Diff == nil {
		return
	}
	deps := domain.AddedDependencies(rel, s.d.Diff(ctx, sess, rel))
	if len(deps) == 0 {
		return
	}
	s.recordLockfile(ctx, sess, c, LockfileFlagRule, domain.ModeWarning, domain.OutcomeWarned, rel, "added "+strings.Join(deps, ", "))
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{LockfileFlag: &ports.LockfileFlagChange{
			ProjectID: sess.ProjectID, SessionID: sess.ID, Lockfile: rel, Manifest: manifest, Dependencies: deps}})
	}
}

func (s *Service) recordLockfile(ctx context.Context, sess *domain.Session, c ports.ArchitectureCheck, rule, mode, outcome, path, detail string) {
	agent := domain.PlainAgent
	if sess.AgentID != "" {
		if a, err := s.d.Agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil && a.Name != "" {
			agent = a.Name
		}
	}
	ev := &domain.ArchitectureRuleEvent{
		At: s.d.Now().UTC(), SessionID: sess.ID, TaskID: sess.TicketID, Agent: agent,
		RuleID: rule, Kind: rule, AtPoint: c.Point, Mode: mode, Outcome: outcome, Path: path, Tool: c.Tool, Detail: detail,
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
