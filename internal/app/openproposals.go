package app

import (
	"context"
	"log/slog"

	"operators-mcp/internal/application/instructionproposal"
)

// openProposals counts a repository's open instruction-file proposals for the
// pages' badges (the first count; the project feed updates it). It counts none
// when proposals are not served or the count cannot be read: a badge that
// misses does not fail a page.
func openProposals(s *instructionproposal.Service) func(ctx context.Context, repositoryID string) int {
	if s == nil {
		return nil
	}
	return func(ctx context.Context, repositoryID string) int {
		_, open, err := s.ListInstructionProposals(ctx, repositoryID, "")
		if err != nil {
			slog.Warn("count a repository's open instruction proposals", "repository", repositoryID, "err", err)
			return 0
		}
		return open
	}
}
