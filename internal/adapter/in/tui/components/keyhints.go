// Package components holds the shared widgets every screen composes: the hint
// bar, stat tiles, the row table, the command palette.
package components

import (
	"strings"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
)

// Hints renders the bottom bar. Compact shows the first few bindings; verbose
// shows them all, wrapping to the width.
func Hints(th theme.Theme, hints []core.KeyHint, width int, verbose bool) string {
	if !verbose && len(hints) > 6 {
		hints = hints[:6]
	}
	key := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	desc := lipgloss.NewStyle().Foreground(th.Muted)
	var parts []string
	for _, h := range hints {
		parts = append(parts, key.Render(h.Key)+" "+desc.Render(h.Desc))
	}
	line := strings.Join(parts, desc.Render("  ·  "))
	if width > 0 {
		return lipgloss.NewStyle().Width(width).Render(line)
	}
	return line
}
