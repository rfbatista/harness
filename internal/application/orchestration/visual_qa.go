package orchestration

import (
	"context"
	"log/slog"

	"operators-mcp/internal/domain"
)

// visualQAOpening is what a qa session's opening adds when its repository
// declares a UI (ADR 0011): the declaration and how to walk it. It is the same
// whoever starts the session, the person through the API or an architect with
// start_task_session. A repository with no declaration, or a settings store
// that cannot be read, adds nothing: the brief is the qa brief it always was.
func (s *Service) visualQAOpening(ctx context.Context, mode domain.SessionMode, projectID, repositoryID string) string {
	if mode != domain.SessionModeQA || s.AgentSystem == nil || repositoryID == "" {
		return ""
	}
	eff, err := s.AgentSystem.Effective(ctx, projectID, repositoryID)
	if err != nil {
		slog.Warn("visual QA declaration unreadable; briefing a plain qa session", "repository", repositoryID, "err", err)
		return ""
	}
	ui := eff.Review.VisualQA.UI.Value
	if ui == nil {
		return ""
	}
	return "\n\n" + domain.VisualQABrief(*ui)
}
