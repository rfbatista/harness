package archrules

import (
	"context"
	"log/slog"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// TestsFailFirstRule names a tests-fail-first refusal in a rule event (ADR 0012).
const TestsFailFirstRule = "tests_fail_first"

// testsFailFirstRefusal denies a gated session's write to a non-test file while
// tests-fail-first is on for it and no gate run of it has had its test gate
// fail: a run of any trigger counts, a practice run limited to test included.
// Writes to test files, and any write of a session without it on or where it is
// not available, are never refused.
func (s *Service) testsFailFirstRefusal(ctx context.Context, c ports.ArchitectureCheck) (ports.ArchitectureVerdict, bool) {
	g, ok := s.guardedWrite(ctx, c, domain.PointBeforeTool)
	if !ok || !g.cfg.TestsFailFirstEnforced() {
		return ports.ArchitectureVerdict{}, false
	}
	i := slices.IndexFunc(g.paths, func(rel string) bool { return !domain.IsTestFile(rel) })
	if i < 0 || s.testRunFailedOrUnknown(ctx, g.sess.ID) {
		return ports.ArchitectureVerdict{}, false
	}
	nonTest := g.paths[i]
	s.recordRule(ctx, g.sess, c, ruleEvent{rule: TestsFailFirstRule, mode: domain.ModeBlocking, outcome: domain.OutcomeBlocked, path: nonTest, detail: "no failed test run yet"})
	return ports.ArchitectureVerdict{Deny: "TESTS_MUST_FAIL_FIRST: " + nonTest + " is not a test file, and no test run of this session has failed yet. " +
		"Write the failing test first, then run it with run_gates (kinds: [\"test\"]) and see it fail; the writes to code open after that."}, true
}

// testRunFailedOrUnknown says whether a gate run of the session has a red test
// gate. It answers true when the runs cannot be read: a store that cannot answer
// never blocks the agent (fail open, on purpose).
func (s *Service) testRunFailedOrUnknown(ctx context.Context, sessionID string) bool {
	if s.d.GateRuns == nil {
		return false
	}
	runs, err := s.d.GateRuns.List(ctx, sessionID)
	if err != nil {
		slog.Warn("a session's gate runs could not be read; tests-fail-first lets its writes through", "session", sessionID, "err", err)
		return true
	}
	return slices.ContainsFunc(runs, func(r domain.GateRun) bool { return r.TestFailed() })
}
