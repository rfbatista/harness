package spawn

import (
	"strings"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/core"
)

// Hints lists the keys for the hint bar.
func (m Model) Hints() []core.KeyHint {
	if m.step == StepDetails {
		return []core.KeyHint{{Key: "tab", Desc: "next field"}, {Key: "enter/ctrl+s", Desc: "start"}, {Key: "esc", Desc: "back"}}
	}
	return []core.KeyHint{{Key: "↵", Desc: "choose"}, {Key: "j/k", Desc: "move"}, {Key: "esc", Desc: "back"}}
}

// View renders the step strip and the picker or form in a focused panel
// sized to the context.
func (m Model) View() string {
	width := min(m.ctx.Width-4, 80)
	th := m.ctx.Theme
	if m.step == StepDetails && m.form != nil {
		return m.form.View(width)
	}
	lines := []string{th.Title().Render("Spawn agent"), m.stepStrip(), ""}
	lines = append(lines, th.Subtitle().Render(m.prompt()))
	if m.loading {
		lines = append(lines, th.Meta().Render("Loading…"))
	}
	if m.err != "" {
		lines = append(lines, th.Error().Render(m.err))
	}
	rows := m.rows()
	if len(rows) == 0 && !m.loading {
		lines = append(lines, th.Subtitle().Render(m.emptyText()))
	}
	for i, r := range rows {
		line := r.label
		if r.hint != "" {
			line += "  " + th.Meta().Render(r.hint)
		}
		if i == m.cursor {
			line = th.Selected().Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", th.Meta().Render("↵ choose  ·  j/k move  ·  esc back"))
	return th.PanelFocused().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// stepStrip shows every step, the current one accented, past ones muted.
func (m Model) stepStrip() string {
	th := m.ctx.Theme
	var strip []string
	for s := StepProject; s <= StepDetails; s++ {
		label := stepLabels[s]
		switch {
		case s == m.step:
			strip = append(strip, th.AccentText().Render(label))
		case s < m.step:
			strip = append(strip, th.Subtitle().Render(label))
		default:
			strip = append(strip, th.Meta().Render(label))
		}
	}
	return strings.Join(strip, th.Meta().Render(" › "))
}
