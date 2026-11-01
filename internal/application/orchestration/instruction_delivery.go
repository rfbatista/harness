package orchestration

import (
	"context"
	"log/slog"
	"path"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

// instructionFiles are the instruction files a session gets delivered through
// llmkit (ADR 0057, contract "llmkit ↔ Harness server — instruction
// delivery"): the repository's files whose applies_to takes in the session's
// agent, and an ARCHITECTURE.md memory file with the summary the brief's
// architecture section holds. It is built once, as the session starts from
// the saved state, so a later save never changes a running session's set.
// A repository with no record has no summary; a session with no repository
// (an architect) gets the summary of the project's records and no files.
func (s *Service) instructionFiles(ctx context.Context, role domain.SessionRole, agent *domain.Agent, projectID, repositoryID string) []llmkit.InstructionFile {
	if s.Architectures == nil || projectID == "" {
		return nil
	}
	var out []llmkit.InstructionFile
	if summary := s.architectureSection(ctx, role, agent, projectID, repositoryID); summary != "" {
		out = append(out, llmkit.InstructionFile{Kind: llmkit.InstructionMemory, Path: "ARCHITECTURE.md", Content: []byte(summary)})
	}
	if repositoryID == "" || role == domain.RoleArchitect {
		return out
	}
	files, err := s.Architectures.InstructionFilesOf(ctx, repositoryID)
	if err != nil {
		slog.Warn("list a repository's instruction files for a session", "repository", repositoryID, "err", err)
		return out
	}
	rec, err := s.Architectures.SavedArchitecture(ctx, repositoryID)
	if err != nil {
		slog.Warn("read a repository's architecture for its instruction files", "repository", repositoryID, "err", err)
		rec = nil
	}
	name := domain.PlainAgent
	if agent != nil && agent.Name != "" {
		name = agent.Name
	}
	skills := skillFolders(files)
	for _, f := range files {
		if !appliesTo(rec, f.Path, name) {
			continue
		}
		kind, ok := deliveryKind(f.Kind)
		if !ok {
			continue
		}
		if kind == llmkit.InstructionSkill && !skills[skillFolder(f.Path)] {
			// llmkit refuses the whole start for a skill folder without SKILL.md.
			slog.Warn("leave a skill file out of a session: its folder has no SKILL.md", "repository", repositoryID, "path", f.Path)
			continue
		}
		out = append(out, llmkit.InstructionFile{Kind: kind, Path: f.Path, Content: []byte(f.Content)})
	}
	return out
}

func deliveryKind(kind string) (llmkit.InstructionKind, bool) {
	switch kind {
	case "memory":
		return llmkit.InstructionMemory, true
	case "skill":
		return llmkit.InstructionSkill, true
	case "subagent":
		return llmkit.InstructionSubAgent, true
	}
	return "", false
}

// appliesTo says whether the record's entry for filePath takes in agent. A
// file with no entry, or an entry with no list, applies to every agent.
func appliesTo(rec *domain.ArchitectureRecord, filePath, agent string) bool {
	if rec == nil {
		return true
	}
	for _, in := range rec.Instructions {
		if in.Path != filePath {
			continue
		}
		return len(in.AppliesTo) == 0 || slices.Contains(in.AppliesTo, "all") || slices.Contains(in.AppliesTo, agent)
	}
	return true
}

// skillFolder is .claude/skills/<name> of a skill file's path.
func skillFolder(p string) string {
	parts := strings.SplitN(p, "/", 4)
	if len(parts) < 4 {
		return p
	}
	return path.Join(parts[:3]...)
}

// skillFolders are the skill folders that hold their SKILL.md.
func skillFolders(files []domain.InstructionFile) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		if f.Kind == "skill" && path.Base(f.Path) == "SKILL.md" && path.Dir(f.Path) == skillFolder(f.Path) {
			out[skillFolder(f.Path)] = true
		}
	}
	return out
}
