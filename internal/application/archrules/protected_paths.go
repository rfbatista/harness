package archrules

import (
	"context"

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
	g, ok := s.guardedWrite(ctx, c, domain.PointBeforeTool)
	if !ok || len(g.cfg.ProtectedPaths) == 0 {
		return ports.ArchitectureVerdict{}, false
	}
	for _, p := range g.paths {
		if glob, hit := domain.ProtectedGlob(g.cfg.ProtectedPaths, p); hit && !scoped(g.cfg.Scope, p) {
			s.recordRule(ctx, g.sess, c, ruleEvent{rule: ProtectedPathRule, mode: domain.ModeBlocking, outcome: domain.OutcomeBlocked, path: p, detail: "protected glob " + glob})
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
