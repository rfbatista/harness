package components

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

// ConfirmedMsg is emitted when the user accepts; Tag names the action.
type ConfirmedMsg struct{ Tag string }

// ConfirmCancelledMsg is emitted when the user backs out.
type ConfirmCancelledMsg struct{ Tag string }

// Confirm is a yes/no prompt for destructive actions.
type Confirm struct {
	th    theme.Theme
	Title string
	Body  string
	Tag   string
}

// NewConfirm builds a prompt; Tag is echoed in the resulting message so the
// caller knows which pending action was confirmed.
func NewConfirm(th theme.Theme, title, body, tag string) Confirm {
	return Confirm{th: th, Title: title, Body: body, Tag: tag}
}

// Update answers y/enter as yes and n/esc/q as no; other keys are ignored.
func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c, nil
	}
	switch k.String() {
	case "y", "Y", "enter":
		tag := c.Tag
		return c, func() tea.Msg { return ConfirmedMsg{Tag: tag} }
	case "n", "N", "esc", "q":
		tag := c.Tag
		return c, func() tea.Msg { return ConfirmCancelledMsg{Tag: tag} }
	}
	return c, nil
}

// View renders the prompt in a focused panel.
func (c Confirm) View(width int) string {
	title := c.th.Error().Bold(true).Render(c.Title)
	body := lipgloss.NewStyle().Foreground(c.th.Ink).Width(width - 4).Render(c.Body)
	keys := c.th.Meta().Render("y confirm  ·  n cancel")
	return c.th.PanelFocused().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", keys))
}
