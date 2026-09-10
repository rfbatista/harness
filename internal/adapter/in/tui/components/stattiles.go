package components

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

// Tile is one stat: a big value over a small label, tinted when it matters.
type Tile struct {
	Label string
	Value string
	Color color.Color // nil uses the ink colour
}

// StatTiles lays tiles out in a row, each boxed, fitting the width.
func StatTiles(th theme.Theme, tiles []Tile, width int) string {
	if len(tiles) == 0 {
		return ""
	}
	per := width/len(tiles) - 2
	if per < 10 {
		per = 10
	}
	box := th.Panel().Width(per)
	label := th.Meta()
	var rendered []string
	for _, t := range tiles {
		c := t.Color
		if c == nil {
			c = th.Ink
		}
		value := lipgloss.NewStyle().Foreground(c).Bold(true).Render(t.Value)
		rendered = append(rendered, box.Render(value+"\n"+label.Render(t.Label)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}
