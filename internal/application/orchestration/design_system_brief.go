package orchestration

import (
	"context"
	"fmt"
	"log/slog"

	"operators-mcp/internal/domain"
)

// designSystemPointer is the brief's part for a session in a repository with
// an applied design system (contract "Harness server ↔ Agent sessions —
// design system view, draft and proposals", "Session brief"): the same text
// for every mode, with the design system's name filled in.
const designSystemPointer = "## Design system\n\n" +
	"This repository uses the project design system \"%s\". Read `project/design-system/README.md` in your virtual shell before you build or change UI. Use its tokens as CSS custom properties or through this repository's own token build, never raw values. The files are read-only: if the design system lacks something, say so in a comment or in your spec."

// designSystemSection is the pointer naming the design system when repo has
// one applied, else nothing.
func designSystemSection(name string, repo *domain.Repository) string {
	if repo == nil || repo.DesignSystemID == nil {
		return ""
	}
	return fmt.Sprintf(designSystemPointer, name)
}

// designSystemBrief reads the repository's applied design system as the
// session starts; the brief is fixed from then on.
func (s *Service) designSystemBrief(ctx context.Context, repositoryID string) string {
	if s.catalog.Repositories == nil || s.catalog.DesignSystems == nil || repositoryID == "" {
		return ""
	}
	repo, err := s.catalog.Repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		slog.Warn("read the repository's design system for a brief", "repository", repositoryID, "err", err)
		return ""
	}
	if repo.DesignSystemID == nil {
		return ""
	}
	ds, err := s.catalog.DesignSystems.GetDesignSystem(ctx, *repo.DesignSystemID)
	if err != nil {
		slog.Warn("read the repository's applied design system for a brief", "repository", repositoryID, "err", err)
		return ""
	}
	return designSystemSection(ds.Name, repo)
}
