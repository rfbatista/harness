package session

import (
	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/core"
)

// barState is one mode of the intervene bar, the State pattern applied to
// the bottom panel: what it draws, which keys it answers to and which hints
// it shows all change together.
//
// The mode is never stored. bar() derives it from the session status and the
// pending approvals on every message, so the bar can never disagree with the
// timeline it sits under.
type barState interface {
	hints() []core.KeyHint
	key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd)
	// view renders the panel's inner content; the caller adds the frame.
	view(m *Model, width int) string
}

// bar picks the mode: ended once the session is terminal, else a question or
// an approval while one is pending, else the composer.
func (m Model) bar() barState {
	status := m.tl.Status
	if m.sess != nil && m.sess.Status.IsTerminal() {
		status = m.sess.Status
	}
	if status.IsTerminal() {
		return endedBar{}
	}
	if req, ok := m.pendingRequest(); ok {
		if req.IsQuestion() {
			return questionBar{}
		}
		return approvalBar{}
	}
	return composerBar{}
}

// barView frames the current mode's content, highlighted when focused.
func (m *Model) barView() string {
	th := m.ctx.Theme
	width := m.ctx.Width
	panel := th.Panel()
	if m.focus == focusBar {
		panel = th.PanelFocused()
	}
	return panel.Width(width).Render(m.bar().view(m, width))
}
