package theme

import (
	"strings"

	"charm.land/glamour/v2"
)

// Markdown renders src with glamour in the theme's standard style, wrapped to
// width. It degrades to the raw text when rendering is impossible, so a bad
// document never blanks a panel.
func (t Theme) Markdown(src string, width int) string {
	if width <= 0 {
		return src
	}
	style := "light"
	if t.IsDark {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(width))
	if err != nil {
		return src
	}
	out, err := r.Render(src)
	if err != nil {
		return src
	}
	return strings.TrimRight(out, "\n")
}
