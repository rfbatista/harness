package archrules

import (
	"context"
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
	g, ok := s.guardedWrite(ctx, c, domain.PointBeforeTool, domain.PointAfterEdit)
	if !ok || !g.cfg.LockfileRule {
		return ports.ArchitectureVerdict{}, false
	}
	for _, rel := range g.paths {
		manifest, isLock := domain.LockfileManifest(rel)
		if !isLock {
			continue
		}
		changed := s.d.ManifestChanged != nil && s.d.ManifestChanged(ctx, g.sess, manifest)
		switch {
		case c.Point == domain.PointBeforeTool && !changed:
			s.recordRule(ctx, g.sess, c, ruleEvent{rule: LockfileRefusalRule, mode: domain.ModeBlocking, outcome: domain.OutcomeBlocked, path: rel, detail: "manifest " + manifest + " unchanged"})
			return ports.ArchitectureVerdict{Deny: "LOCKFILE_WITHOUT_MANIFEST: " + rel + " changes without its manifest " + manifest +
				". Change the manifest first (add or update the dependency there), then let the lockfile follow it."}, true
		case c.Point == domain.PointAfterEdit && changed:
			s.flagLockfile(ctx, g.sess, c, rel, manifest)
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
	s.recordRule(ctx, sess, c, ruleEvent{rule: LockfileFlagRule, mode: domain.ModeWarning, outcome: domain.OutcomeWarned, path: rel, detail: "added " + strings.Join(deps, ", ")})
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{LockfileFlag: &ports.LockfileFlagChange{
			ProjectID: sess.ProjectID, SessionID: sess.ID, Lockfile: rel, Manifest: manifest, Dependencies: deps}})
	}
}
