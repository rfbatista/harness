package orchestration

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The orçamento de memória da sessão is resolved once when a session starts
// or resumes and kept with it (ADR 0008): prevenção and the vigia both read
// it from the session, never from the settings again.

// memoryBudget resolves the orçamento for a session of taskID in
// repositoryID; without a resolver, the global default.
func (s *Service) memoryBudget(ctx context.Context, repositoryID, taskID string) int64 {
	if s.MemoryBudgets == nil {
		return domain.DefaultMemoryBudgetBytes
	}
	return s.MemoryBudgets.ResolveMemoryBudget(ctx, repositoryID, taskID)
}

// withMemoryPreventionAndBrief puts the prevenção de memória for budget into
// cfg's env and the line about --maxWorkers into the agent's brief.
func withMemoryPreventionAndBrief(cfg *llmkit.SessionConfig, budget int64) {
	withMemoryPrevention(cfg, budget)
	cfg.AppendSystem = appendSection(cfg.AppendSystem, memoryPreventionNote)
}

// resumeMemoryBudget resolves a resumed session's orçamento again, under the
// settings now in force, and records it on the session and its struct.
func (s *Service) resumeMemoryBudget(ctx context.Context, sess *domain.Session, cfg *llmkit.SessionConfig) {
	sess.MemoryBudgetBytes = s.memoryBudget(ctx, sess.RepositoryID, sess.TicketID)
	withMemoryPreventionAndBrief(cfg, sess.MemoryBudgetBytes)
	if err := s.sessions.UpdateMemoryBudget(sess.ID, sess.MemoryBudgetBytes); err != nil {
		slog.Warn("memory budget not recorded for the resumed session", "session", sess.ID, "err", err)
	}
}

// Prevenção de memória (ADR 0007): the limits derived from a session's
// orçamento that the harness puts into its environment, so a test runner is
// held under it before the vigia has to end anything.

const (
	// testWorkerBytes is the budget one test worker is planned for.
	testWorkerBytes = int64(2) << 30
	// maxOldSpace is the Node heap ceiling option.
	maxOldSpace = "--max-old-space-size"
)

// testWorkers is max(1, ⌊orçamento ÷ 2 GB⌋).
func testWorkers(budget int64) int64 { return max(1, budget/testWorkerBytes) }

// heapCeilingMB is ⌊orçamento ÷ (workers + 1)⌋ in MB: each worker and the
// parent process may use as much as the others.
func heapCeilingMB(budget int64) int64 {
	return (budget >> 20) / (testWorkers(budget) + 1)
}

// withMemoryPrevention puts the prevenção de memória for budget into cfg's
// env: NODE_OPTIONS with a heap ceiling, HARNESS_TEST_WORKERS and the Vitest
// worker variables. NODE_OPTIONS the session's env already has (or the
// server's, which the agent inherits) is kept, and the ceiling is added only
// when it carries no --max-old-space-size. It is called where the env still
// goes through the sandbox, and on resume with the orçamento then in force.
func withMemoryPrevention(cfg *llmkit.SessionConfig, budget int64) {
	if budget <= 0 {
		return
	}
	workers := strconv.FormatInt(testWorkers(budget), 10)
	withEnv(cfg,
		"NODE_OPTIONS="+nodeOptionsWithCeiling(existingNodeOptions(cfg.Env), budget),
		"HARNESS_TEST_WORKERS="+workers,
		"VITEST_MAX_WORKERS="+workers,
		"VITEST_MAX_THREADS="+workers,
		"VITEST_MAX_FORKS="+workers,
	)
}

// existingNodeOptions are the NODE_OPTIONS the agent would run with without
// us: the session env's, else the server's.
func existingNodeOptions(env []string) string {
	if v := sessionEnvValue(env, "NODE_OPTIONS"); v != "" {
		return v
	}
	return os.Getenv("NODE_OPTIONS")
}

// nodeOptionsWithCeiling is existing with the heap ceiling for budget added,
// unless it already sets one.
func nodeOptionsWithCeiling(existing string, budget int64) string {
	if strings.Contains(existing, maxOldSpace) {
		return existing
	}
	ceiling := maxOldSpace + "=" + strconv.FormatInt(heapCeilingMB(budget), 10)
	if strings.TrimSpace(existing) == "" {
		return ceiling
	}
	return strings.TrimSpace(existing) + " " + ceiling
}

// memoryPreventionNote tells the agent how to hold its test runners to the
// orçamento; the numbers are in its environment.
const memoryPreventionNote = "Memory: this session has a memory budget. Run tests with a bounded worker count: pass `--maxWorkers=$HARNESS_TEST_WORKERS` to Jest and Vitest (for example `npx jest --maxWorkers=$HARNESS_TEST_WORKERS`). If a process of yours is ended for passing the budget, the tool result or a message says so: run fewer workers rather than retrying the same command."

// watchMemory hands a session to the vigia de memória for the orçamento it
// started with, and has its end stop the watch. rootPID is the PTY's process,
// 0 for a headless session.
func (s *Service) watchMemory(id, taskID string, budget int64, claudeID string, rootPID int) {
	if s.MemoryWatcher == nil || budget <= 0 {
		return
	}
	stop := s.MemoryWatcher.Watch(ports.MemoryWatch{SessionID: id, TaskID: taskID, BudgetBytes: budget, ClaudeID: claudeID, RootPID: rootPID})
	s.mu.Lock()
	prevCleanup := s.cleanups[id]
	s.cleanups[id] = func() {
		stop()
		if prevCleanup != nil {
			prevCleanup()
		}
	}
	s.mu.Unlock()
}

// watchTerminalMemory watches a session the server runs on a terminal, by the
// POSIX session of that terminal's process.
func (s *Service) watchTerminalMemory(id string) {
	if s.MemoryWatcher == nil || s.Terminals == nil {
		return
	}
	sess, err := s.interactiveSession(id)
	if err != nil {
		return
	}
	root := 0
	if t, err := s.Terminals.Attach(id); err == nil {
		if p, ok := t.(interface{ PID() int }); ok {
			root = p.PID()
		}
	}
	s.watchMemory(id, sess.TicketID, sess.MemoryBudgetBytes, claudeID(sess), root)
}
