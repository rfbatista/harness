package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
)

// The chrome is everything around the body: the nav rail on the left, the
// header line above and the hint bar below.

const (
	navWidthWide   = 16
	navWidthNarrow = 4
	narrowBelow    = 100
)

func (m Model) navWidth() int {
	if m.width > 0 && m.width < narrowBelow {
		return navWidthNarrow
	}
	return navWidthWide
}

// header shows the section title and subtitle on the left, the toast on the right.
func (m Model) header(width int) string {
	title := m.th.Title().Render(m.section.Label())
	sub := m.th.Subtitle().Render(" · " + m.current().Summary())
	if m.active != "" {
		title = m.th.Title().Render("Session")
		sub = m.th.Subtitle().Render(" · esc returns to " + m.section.Label())
	}
	right := ""
	if m.toast != "" {
		style := m.th.Subtitle()
		if m.toastErr {
			style = m.th.Error()
		}
		right = style.Render(m.toast)
	}
	left := title + sub
	pad := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", pad) + right
}

// globalHints are appended to every screen's own hints.
var globalHints = []core.KeyHint{
	{Key: "ctrl+k", Desc: "search"},
	{Key: "tab", Desc: "next section"},
	{Key: "1-7", Desc: "jump"},
	{Key: "r", Desc: "refresh"},
	{Key: "d", Desc: "log"},
	{Key: "?", Desc: "help"},
	{Key: "q", Desc: "quit"},
}

func (m Model) hints() []core.KeyHint {
	if s, ok := m.shownSession(); ok {
		return append(s.Hints(), core.KeyHint{Key: "ctrl+c", Desc: "quit"})
	}
	return append(m.current().Hints(), globalHints...)
}

func (m Model) hintBar() string {
	return components.Hints(m.th, m.hints(), m.width, m.showHelp)
}

var sectionIcons = map[core.Section]string{
	core.SectionTasks: "▤", core.SectionInbox: "◔", core.SectionHistory: "◷",
	core.SectionAgents: "◉", core.SectionSkills: "✦", core.SectionMCPs: "⌬", core.SectionSettings: "⚙",
}

// rail draws the nav column with per-section counts. Below narrowBelow
// columns it collapses to icons.
func (m Model) rail(height int) string {
	narrow := m.navWidth() == navWidthNarrow
	brand := "● coding_pool"
	if narrow {
		brand = "●"
	}
	lines := []string{m.th.AccentText().Render(brand), ""}
	for _, s := range core.Sections() {
		label := sectionIcons[s]
		if !narrow {
			label += " " + s.Label()
			if n := m.count(s); n > 0 {
				label = fmt.Sprintf("%s %d", label, n)
			}
		}
		style := lipgloss.NewStyle().Foreground(m.th.Muted).Width(m.navWidth())
		if s == m.section {
			style = style.Foreground(m.th.Ink).Bold(true).Background(m.th.Surface2)
		}
		lines = append(lines, style.Render(label))
		if s == core.SectionHistory {
			lines = append(lines, "") // gap between the task sections and the catalogs
		}
	}
	rail := lipgloss.NewStyle().Width(m.navWidth()).Height(height).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderRight(true).BorderForeground(m.th.Border).Render(rail)
}

// count is the badge for a section: tasks, attention items, completed tasks,
// and catalog sizes.
func (m Model) count(s core.Section) int {
	switch s {
	case core.SectionTasks:
		return len(m.snap.Tickets)
	case core.SectionInbox:
		return len(rollup.Join(m.snap.Tickets, m.snap.Sessions, "", rollup.FilterAttention))
	case core.SectionHistory:
		return len(rollup.Join(m.snap.Tickets, m.snap.Sessions, "", rollup.FilterDone))
	case core.SectionAgents:
		return len(m.snap.Agents)
	case core.SectionSkills:
		return len(m.snap.Skills)
	case core.SectionMCPs:
		return len(m.snap.MCPServers)
	case core.SectionSettings:
		return len(m.snap.Projects)
	}
	return 0
}
