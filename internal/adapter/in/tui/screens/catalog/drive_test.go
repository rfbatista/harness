package catalog

import (
	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
)

// drive runs a command to completion, feeding screen-bound messages back into
// the model and collecting the ones addressed to the root (refresh, toast).
func drive(m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	var root []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch msg := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
			continue
		case core.RefreshMsg, core.ToastMsg, core.NavigateMsg:
			root = append(root, msg)
			continue
		case nil:
			continue
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		queue = append(queue, next)
	}
	return m, root
}

func typeKeys(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func press(m Model, k tea.KeyPressMsg) (Model, []tea.Msg) {
	m, cmd := m.Update(k)
	return drive(m, cmd)
}

func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func esc() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func tab() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyTab} }
func ch(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func hasRefresh(msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(core.RefreshMsg); ok {
			return true
		}
	}
	return false
}

func withSnap(m Model, f *backend.Fake) Model {
	m, _ = m.Update(core.SnapshotMsg{Snapshot: backend.Load(f)})
	return m
}
