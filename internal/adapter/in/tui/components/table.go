package components

import (
	"strings"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

// Column is a table heading with a fixed width; a zero width takes the remainder.
type Column struct {
	Title string
	Width int
}

// Table is a cursor over rows of plain cells. It owns no data source: callers
// SetRows on every snapshot and the cursor is clamped to stay valid.
type Table struct {
	Columns []Column
	rows    [][]string
	cursor  int
	offset  int
}

// NewTable builds an empty table with the given columns.
func NewTable(cols []Column) Table { return Table{Columns: cols} }

// SetRows replaces the rows and keeps the cursor in range.
func (t *Table) SetRows(rows [][]string) {
	t.rows = rows
	t.clamp()
}

// Len is the row count.
func (t Table) Len() int { return len(t.rows) }

// Cursor is the selected row index, or -1 when empty.
func (t Table) Cursor() int {
	if len(t.rows) == 0 {
		return -1
	}
	return t.cursor
}

// Move shifts the cursor by delta, clamped.
func (t *Table) Move(delta int) {
	t.cursor += delta
	t.clamp()
}

// First selects the first row.
func (t *Table) First() { t.cursor = 0; t.clamp() }

// Last selects the last row.
func (t *Table) Last() { t.cursor = len(t.rows) - 1; t.clamp() }

func (t *Table) clamp() {
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.offset > t.cursor {
		t.offset = t.cursor
	}
}

// View renders the header plus as many rows as fit in height lines.
func (t *Table) View(th theme.Theme, width, height int) string {
	widths := t.widths(width)
	header := th.Meta().Bold(true).Render(t.line(t.titles(), widths))
	bodyHeight := height - 1
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	if t.cursor >= t.offset+bodyHeight {
		t.offset = t.cursor - bodyHeight + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}
	var lines []string
	lines = append(lines, header)
	end := t.offset + bodyHeight
	if end > len(t.rows) {
		end = len(t.rows)
	}
	for i := t.offset; i < end; i++ {
		line := t.line(t.rows[i], widths)
		if i == t.cursor {
			line = th.Selected().Render(line)
		} else {
			line = lipgloss.NewStyle().Foreground(th.Ink).Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (t Table) titles() []string {
	out := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		out[i] = c.Title
	}
	return out
}

// widths resolves the zero-width column to whatever the fixed ones leave over.
func (t Table) widths(total int) []int {
	out := make([]int, len(t.Columns))
	used, flex := 0, -1
	for i, c := range t.Columns {
		if c.Width == 0 {
			flex = i
			continue
		}
		out[i] = c.Width
		used += c.Width + 1
	}
	if flex >= 0 {
		rest := total - used
		if rest < 8 {
			rest = 8
		}
		out[flex] = rest
	}
	return out
}

// line pads or truncates each cell to its column width. Cells may carry ANSI
// styling, so widths are measured with lipgloss rather than len.
func (t Table) line(cells []string, widths []int) string {
	parts := make([]string, len(widths))
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		parts[i] = fit(cell, w)
	}
	return strings.Join(parts, " ")
}

// fit truncates with an ellipsis or pads with spaces to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	cur := lipgloss.Width(s)
	if cur > w {
		return lipgloss.NewStyle().MaxWidth(w).Render(s)
	}
	return s + strings.Repeat(" ", w-cur)
}
