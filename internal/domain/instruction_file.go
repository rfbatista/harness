package domain

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// Limits of the instruction files of one repository: each file is at most
// 256 KB, and all of them together at most 4 MiB, the llmkit limit.
const (
	MaxInstructionFileSize  = 256 * 1024
	MaxInstructionFilesSize = 4 << 20
)

// InstructionFile is one instruction file the harness keeps for a repository:
// a memory file (AGENTS.md, CLAUDE.md, GEMINI.md), one file of a skill, or a
// sub-agent prompt (ADR 0057). Path is repository-relative with "/" separators.
type InstructionFile struct {
	RepositoryID string
	ProjectID    string
	Path         string
	Kind         string // memory | skill | subagent
	Version      int
	// Size is the content's size in bytes; Content is empty in a list.
	Size      int
	Content   string
	UpdatedAt time.Time
}

// ValidateInstructionFile lists what is wrong with a file about to be saved:
// its path, its kind against the path's pattern, and its size. The error paths
// are JSON pointers into the save request (/path, /kind, /content).
func ValidateInstructionFile(filePath, kind, content string) []ArchFieldError {
	var out []ArchFieldError
	if msg := InstructionPathProblem(filePath); msg != "" {
		out = append(out, ArchFieldError{Path: "/path", Code: ArchInvalidValue, Message: msg})
	}
	if !oneOf(kind, "memory", "skill", "subagent") {
		out = append(out, ArchFieldError{Path: "/kind", Code: ArchInvalidValue, Message: "kind must be memory, skill or subagent"})
	} else if filePath != "" && InstructionPathProblem(filePath) == "" {
		if msg := instructionKindProblem(filePath, kind); msg != "" {
			out = append(out, ArchFieldError{Path: "/path", Code: ArchInvalidValue, Message: msg})
		}
	}
	if len(content) > MaxInstructionFileSize {
		out = append(out, ArchFieldError{Path: "/content", Code: ArchInvalidValue, Message: fmt.Sprintf("a file is at most %d KB, this one is %d bytes", MaxInstructionFileSize/1024, len(content))})
	}
	return out
}

// InstructionPathProblem says why p cannot be an instruction file's path:
// empty when it can.
func InstructionPathProblem(p string) string {
	switch {
	case p == "":
		return "path is required"
	case strings.HasPrefix(p, "/") || strings.Contains(p, "\\"):
		return "path must be relative to the repository, with / separators"
	case path.Clean(p) != p:
		return "path must be clean: no .., ./, // or trailing /"
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "..":
			return "path must not contain .."
		case ".git":
			return "path must not be in .git"
		}
	}
	return ""
}

func instructionKindProblem(p, kind string) string {
	parts := strings.Split(p, "/")
	switch kind {
	case "memory":
		if !oneOf(path.Base(p), "AGENTS.md", "CLAUDE.md", "GEMINI.md") {
			return "a memory file is AGENTS.md, CLAUDE.md or GEMINI.md, at any path"
		}
	case "skill":
		if len(parts) < 4 || parts[0] != ".claude" || parts[1] != "skills" {
			return "a skill file is .claude/skills/<name>/<file>"
		}
	case "subagent":
		if len(parts) != 3 || parts[0] != ".claude" || parts[1] != "agents" || !strings.HasSuffix(parts[2], ".md") || parts[2] == ".md" {
			return "a sub-agent file is .claude/agents/<name>.md"
		}
	}
	return ""
}
