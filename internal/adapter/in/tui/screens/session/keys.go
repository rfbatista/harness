package session

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/components"
)

// confirmDelete tags the delete confirm so its answer is recognised.
const confirmDelete = "delete"

// key resolves who owns the keyboard: an open confirm, then the feed when it
// is focused, otherwise the intervene bar in its current mode.
func (m Model) key(k tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.confirm != nil {
		c, cmd := m.confirm.Update(k)
		m.confirm = &c
		return m, cmd
	}
	if m.focus == focusFeed {
		return m.feedKey(k)
	}
	return m.bar().key(m, k)
}

// controlKey handles the session controls that work in every mode: esc goes
// back, a toggles auto-run, S stops, D asks to delete. handled=false means
// the key is not one of them.
func (m Model) controlKey(k tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	switch k.String() {
	case "esc":
		return m, func() tea.Msg { return BackMsg{} }, true
	case "a":
		if m.sess == nil {
			return m, nil, true
		}
		enabled := !m.sess.AutoRun
		m.sess.AutoRun = enabled
		return m, m.action(actionAutoRun, func(ctx context.Context) error { return m.be.SetAutoRun(ctx, m.id, enabled) }), true
	case "S":
		return m, m.action(actionStop, func(ctx context.Context) error { return m.be.Stop(ctx, m.id) }), true
	case "D":
		c := components.NewConfirm(m.ctx.Theme, "Delete session", "Delete this session and its history? A running agent is stopped first.", confirmDelete)
		m.confirm = &c
		return m, nil, true
	}
	return m, nil, false
}

// feedKey moves through the timeline; any movement pauses tail following
// until G resumes it.
func (m Model) feedKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	if next, cmd, ok := m.controlKey(k); ok {
		return next, cmd
	}
	n := len(m.tl.Items)
	switch k.String() {
	case "tab":
		m.focus = focusBar
	case "j", "down":
		if m.cursor < n-1 {
			m.cursor++
		}
		m.follow = false
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		m.follow = false
	case "g":
		m.cursor, m.follow = 0, false
	case "G":
		m.cursor, m.follow = n-1, true
	case "ctrl+d", "pgdown":
		m.vp.HalfPageDown()
		m.follow = false
		return m, nil
	case "ctrl+u", "pgup":
		m.vp.HalfPageUp()
		m.follow = false
		return m, nil
	case "enter", " ":
		if m.cursor >= 0 && m.cursor < n {
			seq := m.tl.Items[m.cursor].Seq
			m.expanded[seq] = !m.expanded[seq]
		}
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.layout()
	return m, nil
}

// focusFeedAtTail moves the keyboard to the feed with the newest item selected.
func (m *Model) focusFeedAtTail() {
	m.focus = focusFeed
	m.cursor = len(m.tl.Items) - 1
}
