package spawn

import (
	"regexp"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/components"
)

func editorDone(tag, content string) components.EditorDoneMsg {
	return components.EditorDoneMsg{Tag: tag, Content: content}
}

// clearField wipes the focused text field with backspaces.
func clearField(m Model) Model {
	for i := 0; i < 60; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	return m
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }
