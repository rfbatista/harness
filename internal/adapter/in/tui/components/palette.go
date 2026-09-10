package components

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
)

// PaletteItem is one searchable thing: what it is, how it reads, and its id.
type PaletteItem struct {
	Kind  core.EntityKind
	Label string
	Hint  string
	ID    string
}

// PaletteChosenMsg is emitted when the user picks an item.
type PaletteChosenMsg struct{ Item PaletteItem }

// Palette is the Ctrl+K fuzzy finder over every entity the TUI knows.
type Palette struct {
	th     theme.Theme
	items  []PaletteItem
	input  textinput.Model
	cursor int
}

// NewPalette builds a focused palette over items.
func NewPalette(th theme.Theme, items []PaletteItem) Palette {
	in := textinput.New()
	in.Prompt = "Search  "
	in.Placeholder = "tasks, agents, skills, MCP servers, projects"
	in.Focus()
	return Palette{th: th, items: items, input: in}
}

// Cursor is the highlighted match index.
func (p Palette) Cursor() int { return p.cursor }

// Matches returns the items whose label or kind contains the typed characters
// in order (case-insensitive subsequence), in original order.
func (p Palette) Matches() []PaletteItem {
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	if q == "" {
		return p.items
	}
	var out []PaletteItem
	for _, it := range p.items {
		if subsequence(strings.ToLower(it.Label), q) || subsequence(strings.ToLower(string(it.Kind)+" "+it.Label), q) {
			out = append(out, it)
		}
	}
	return out
}

func subsequence(s, q string) bool {
	i := 0
	for _, r := range s {
		if i < len(q) && rune(q[i]) == r {
			i++
		}
	}
	return i == len(q)
}

// Update handles navigation keys itself and forwards typing to the input.
func (p Palette) Update(msg tea.Msg) (Palette, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "down", "ctrl+n":
			p.cursor++
			p.clamp()
			return p, nil
		case "up", "ctrl+p":
			p.cursor--
			p.clamp()
			return p, nil
		case "enter":
			m := p.Matches()
			if len(m) == 0 {
				return p, nil
			}
			p.clamp()
			item := m[p.cursor]
			return p, func() tea.Msg { return PaletteChosenMsg{Item: item} }
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.clamp()
	return p, cmd
}

func (p *Palette) clamp() {
	n := len(p.Matches())
	if p.cursor >= n {
		p.cursor = n - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// View renders the input and up to ten matches inside a panel.
func (p Palette) View(width int) string {
	m := p.Matches()
	kind := p.th.Meta()
	var lines []string
	lines = append(lines, p.input.View())
	lines = append(lines, "")
	if len(m) == 0 {
		lines = append(lines, p.th.Subtitle().Render("No matches"))
	}
	for i, it := range m {
		if i >= 10 {
			lines = append(lines, kind.Render("…"))
			break
		}
		row := fit(kind.Render(string(it.Kind)), 8) + " " + it.Label
		if it.Hint != "" {
			row += "  " + kind.Render(it.Hint)
		}
		if i == p.cursor {
			row = p.th.Selected().Render(fit(row, width-4))
		}
		lines = append(lines, row)
	}
	return p.th.PanelFocused().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
