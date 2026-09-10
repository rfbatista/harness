package session

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/core"
)

// endedBar is shown once the session is terminal: the final status and cost.
type endedBar struct{}

func (endedBar) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "esc", Desc: "back"}, {Key: "tab", Desc: "to feed"}, {Key: "D", Desc: "delete"}}
}

func (endedBar) key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) {
	if next, cmd, ok := m.controlKey(k); ok {
		return next, cmd
	}
	if k.String() == "tab" {
		m.focusFeedAtTail()
	}
	return m, nil
}

func (endedBar) view(m *Model, _ int) string {
	th := m.ctx.Theme
	cost := ""
	if m.sess != nil {
		cost = fmt.Sprintf(" · $%.2f", m.sess.CostUSD)
	}
	return th.Subtitle().Render("Session "+string(m.liveStatus())) + th.Meta().Render(cost+"  ·  esc back  ·  D delete")
}
