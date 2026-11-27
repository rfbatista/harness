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
	// A write after a cd the text cannot resolve could land on a protected path,
	// so it is refused: write it from a plain directory.
	if len(c.ShellUnresolved) > 0 {
		target := c.ShellUnresolved[0]
		s.recordRule(ctx, g.sess, c, ruleEvent{rule: ProtectedPathRule, mode: domain.ModeBlocking, outcome: domain.OutcomeBlocked, path: target, detail: "write after a cd the harness cannot resolve"})
		return ports.ArchitectureVerdict{Deny: "PROTECTED_PATH: this command writes " + target + " after a cd to a directory the harness cannot work out (a variable, a substitution, ~ or -), so it could be a protected path. " +
			"Change directory to a plain path first, or write the file by its path from the worktree."}, true
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
