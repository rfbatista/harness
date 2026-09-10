package components

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// EditorDoneMsg carries the content back from an external editor session.
// Tag identifies which field or file asked for the edit.
type EditorDoneMsg struct {
	Tag     string
	Content string
	Err     error
}

// EditInEditor suspends the TUI, opens $EDITOR (or $VISUAL, or vi) on a temp
// file holding initial, and reports the result as an EditorDoneMsg. This is
// glow's model: the terminal is handed to a real editor rather than
// reimplementing one.
func EditInEditor(initial, ext, tag string) tea.Cmd {
	s, err := newEditSession(initial, ext)
	if err != nil {
		return func() tea.Msg { return EditorDoneMsg{Tag: tag, Err: err} }
	}
	parts := strings.Fields(editorCommand())
	cmd := exec.Command(parts[0], append(parts[1:], s.path)...) //nolint:gosec // the user's own editor
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return s.collect(tag, err) })
}

// editorCommand honours EDITOR, then VISUAL, then falls back to vi.
func editorCommand() string {
	for _, key := range []string{"EDITOR", "VISUAL"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return "vi"
}

// editSession is one temp file handed to the editor.
type editSession struct {
	path string
}

func newEditSession(initial, ext string) (*editSession, error) {
	f, err := os.CreateTemp("", "coding-pool-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("editor: create temp file: %w", err)
	}
	if _, err := f.WriteString(initial); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, fmt.Errorf("editor: write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return nil, fmt.Errorf("editor: close temp file: %w", err)
	}
	return &editSession{path: f.Name()}, nil
}

// collect reads the edited file, removes it, and wraps any editor failure.
func (s *editSession) collect(tag string, runErr error) EditorDoneMsg {
	defer s.cleanup()
	if runErr != nil {
		return EditorDoneMsg{Tag: tag, Err: fmt.Errorf("editor exited with error: %w", runErr)}
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return EditorDoneMsg{Tag: tag, Err: fmt.Errorf("editor: read result: %w", err)}
	}
	return EditorDoneMsg{Tag: tag, Content: string(b)}
}

func (s *editSession) cleanup() { os.Remove(s.path) }
