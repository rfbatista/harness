// Package claudehome reads what the claude CLI keeps in its config directory
// (~/.claude, or $CLAUDE_CONFIG_DIR).
package claudehome

import (
	"os"
	"path/filepath"
	"regexp"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

var _ ports.ClaudeTranscripts = Transcripts{}

// Transcripts finds conversation transcripts. The CLI stores each one at
// <config>/projects/<encoded cwd>/<conversation id>.jsonl, where the encoded
// cwd is the directory with every non-alphanumeric character turned into "-".
type Transcripts struct {
	// Dir is the CLI's config directory; empty means $CLAUDE_CONFIG_DIR, else
	// ~/.claude.
	Dir string
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// CanResume reports whether conversation id, started in dir, can be resumed.
func (t Transcripts) CanResume(dir, id string) error {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "the session's worktree no longer exists: " + dir}
	}
	root := t.root()
	// The CLI may record the directory as given or with symlinks resolved
	// (/tmp against /private/tmp on macOS), so both spellings are accepted.
	candidates := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		candidates = append(candidates, real)
	}
	for _, d := range candidates {
		p := filepath.Join(root, "projects", nonAlnum.ReplaceAllString(d, "-"), id+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return nil
		}
	}
	return &domain.StructuredError{Code: "SESSION_TRANSCRIPT_MISSING", Message: "claude has no saved conversation " + id + " for " + dir}
}

func (t Transcripts) root() string {
	if t.Dir != "" {
		return t.Dir
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}
