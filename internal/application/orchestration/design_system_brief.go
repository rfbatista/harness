package orchestration

import (
	"context"
	"log/slog"

	"operators-mcp/internal/domain"
)

// designSystemPointer is the brief's part for a session in a repository that
// opted into the project design system (contract "Harness server ↔ Agent
// sessions — design system view, draft and proposals", "Session brief"): the
// same text for every mode.
const designSystemPointer = "## Design system\n\n" +
	"This repository uses the project design system. Read `project/design-system/README.md` in your virtual shell before you build or change UI. Use its tokens as CSS custom properties or through this repository's own token build, never raw values. The files are read-only: if the design system lacks something, say so in a comment or in your spec."

// designSystemSection is the pointer when repo opted in, else nothing.
func designSystemSection(repo *domain.Repository) string {
	if repo == nil || repo.DesignSystemID == nil { // ticket 12 re-key this
		return ""
	}
	return designSystemPointer
}

// designSystemBrief reads the repository's flag as the session starts; the
// brief is fixed from then on.
func (s *Service) designSystemBrief(ctx context.Context, repositoryID string) string {
	if s.catalog.Repositories == nil || repositoryID == "" {
		return ""
	}
	repo, err := s.catalog.Repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		slog.Warn("read the repository's design system opt-in for a brief", "repository", repositoryID, "err", err)
		return ""
	}
	return designSystemSection(repo)
}
