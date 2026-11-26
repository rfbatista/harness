package orchestration

import (
	"context"
	"log/slog"

	"operators-mcp/internal/domain"
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

// resumeMemoryBudget resolves a resumed session's orçamento again, under the
// settings now in force, and records it on the session and its struct.
func (s *Service) resumeMemoryBudget(ctx context.Context, sess *domain.Session) {
	sess.MemoryBudgetBytes = s.memoryBudget(ctx, sess.RepositoryID, sess.TicketID)
	if err := s.sessions.UpdateMemoryBudget(sess.ID, sess.MemoryBudgetBytes); err != nil {
		slog.Warn("memory budget not recorded for the resumed session", "session", sess.ID, "err", err)
	}
}
