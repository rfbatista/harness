package session

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/core"
)

// composerBar is the default mode: a textarea that sends a message to the
// agent while the session is idle or thinking.
type composerBar struct{}

func (composerBar) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "ctrl+s", Desc: "send"}, {Key: "tab", Desc: "to feed"}, {Key: "esc", Desc: "back"}}
}

func (composerBar) key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		return m, func() tea.Msg { return BackMsg{} }
	case "tab":
		m.focusFeedAtTail()
		return m, nil
	case "ctrl+s", "ctrl+enter":
		text := strings.TrimSpace(m.composer.Value())
		if text == "" || m.sending {
			return m, nil
		}
		m.sending = true
		return m, m.action(actionSend, func(ctx context.Context) error { return m.be.Send(ctx, m.id, text) })
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(k)
	return m, cmd
}

func (composerBar) view(m *Model, width int) string {
	th := m.ctx.Theme
	m.composer.SetWidth(width - 4)
	m.composer.SetHeight(3)
	lines := []string{m.composer.View()}
	switch {
	case m.sending:
		lines = append(lines, th.Meta().Render("Sending…"))
	case m.sendErr != "":
		lines = append(lines, th.Error().Render(m.sendErr))
	default:
		lines = append(lines, th.Meta().Render("ctrl+s send  ·  tab feed  ·  esc back"))
	}
	return strings.Join(lines, "\n")
}
