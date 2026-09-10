package catalog

import (
	"regexp"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/components"
)

func tea_down() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown} }

func editorDone(tag, content string) components.EditorDoneMsg {
	return components.EditorDoneMsg{Tag: tag, Content: content}
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips ANSI styling so substring checks see the text as a user would.
func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }
