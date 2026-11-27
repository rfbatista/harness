package orchestration

import (
	"context"
	"log/slog"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

// ContextMapFile is the path the context map is delivered at, beside the
// instruction files and never repeating AGENTS.md.
const ContextMapFile = "CONTEXT-MAP.md"

// contextMapFile is the repository's context map as a memory file for a
// session starting on it. It is read once, as the session starts, and the
// enabled flag with it; a disabled map, an unmapped repository and a session
// with no repository have none.
func (s *Service) contextMapFile(ctx context.Context, role domain.SessionRole, repositoryID string) (llmkit.InstructionFile, bool) {
	if s.ContextMaps == nil || repositoryID == "" || role == domain.RoleArchitect {
		return llmkit.InstructionFile{}, false
	}
	m, err := s.ContextMaps.GetContextMap(ctx, repositoryID)
	if err != nil {
		slog.Warn("read a repository's context map for a session", "repository", repositoryID, "err", err)
		return llmkit.InstructionFile{}, false
	}
	if !m.Enabled || !m.Available {
		return llmkit.InstructionFile{}, false
	}
	return llmkit.InstructionFile{Kind: llmkit.InstructionMemory, Path: ContextMapFile, Content: []byte(m.Content)}, true
}
