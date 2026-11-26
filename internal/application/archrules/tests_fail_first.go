package archrules

import (
	"context"
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
	if s.d.Guard == nil || c.Point != domain.PointBeforeTool || c.Person {
		return ports.ArchitectureVerdict{}, false
	}
	sess := s.d.Sessions.Get(c.SessionID)
	if sess == nil || sess.WorkingDir == "" {
		return ports.ArchitectureVerdict{}, false
	}
	cfg, ok, err := s.d.Guard.Config(ctx, sess.ID)
	if err != nil || !ok || !cfg.TestsFailFirstEnforced() || !domain.SessionGated(cfg.Mode, true) {
		return ports.ArchitectureVerdict{}, false
	}
	var nonTest string
	for _, rel := range slices.Concat(inside(sess.WorkingDir, c.Paths), inside(sess.WorkingDir, c.ShellWrites)) {
		if !domain.IsTestFile(rel) {
			nonTest = rel
			break
		}
	}
	if nonTest == "" || s.testHasFailed(ctx, sess.ID) {
		return ports.ArchitectureVerdict{}, false
	}
	s.recordTestsFirst(ctx, sess, c, nonTest)
	return ports.ArchitectureVerdict{Deny: "TESTS_MUST_FAIL_FIRST: " + nonTest + " is not a test file, and no test run of this session has failed yet. " +
		"Write the failing test first, then run it with run_gates (kinds: [\"test\"]) and see it fail; the writes to code open after that."}, true
}

// testHasFailed says whether a gate run of the session has a red test gate.
func (s *Service) testHasFailed(ctx context.Context, sessionID string) bool {
	if s.d.GateRuns == nil {
		return false
	}
	runs, err := s.d.GateRuns.List(ctx, sessionID)
	if err != nil {
		return true // a store that cannot answer never blocks the agent
	}
	return slices.ContainsFunc(runs, func(r domain.GateRun) bool {
		return slices.ContainsFunc(r.Gates, func(g domain.GateResult) bool {
			return g.Kind == domain.GateTest && !g.Passed() && !g.Skipped
		})
	})
}

func (s *Service) recordTestsFirst(ctx context.Context, sess *domain.Session, c ports.ArchitectureCheck, path string) {
	s.recordLockfile(ctx, sess, c, TestsFailFirstRule, domain.ModeBlocking, domain.OutcomeBlocked, path, "no failed test run yet")
}
