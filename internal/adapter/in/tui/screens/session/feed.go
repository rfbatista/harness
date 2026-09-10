package session

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/rollup"
	"operators-mcp/internal/domain"
)

// feedHeight is what the window leaves for the timeline once the header and
// the intervene bar have taken theirs.
func (m *Model) feedHeight() int {
	return max(m.ctx.Height-3-lipgloss.Height(m.barView())-1, 4)
}

// layout re-renders the feed into the viewport and keeps the cursor or the
// tail visible.
func (m *Model) layout() {
	m.vp.SetWidth(m.ctx.Width)
	m.vp.SetHeight(m.feedHeight())
	content, offsets := m.renderFeed()
	m.vp.SetContent(content)
	switch {
	case m.focus == focusFeed && m.cursor >= 0 && m.cursor < len(offsets):
		m.vp.SetYOffset(offsets[m.cursor] - m.vp.Height()/2)
	case m.follow:
		m.vp.GotoBottom()
	}
}

// renderFeed draws every item, then the streaming tail or a working marker,
// then the usage line. It returns the line offset of each item so layout can
// scroll to the cursor.
func (m *Model) renderFeed() (string, []int) {
	th := m.ctx.Theme
	width := m.ctx.Width - 2
	var b strings.Builder
	offsets := make([]int, 0, len(m.tl.Items))
	lines := 0
	write := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
		lines += strings.Count(s, "\n") + 1
	}
	for i, it := range m.tl.Items {
		offsets = append(offsets, lines)
		selected := m.focus == focusFeed && i == m.cursor
		write(m.renderItem(it, selected, width))
	}
	switch {
	case !m.deltas.Empty():
		if thinking := m.deltas.Thinking(); thinking != "" {
			write(th.Meta().Italic(true).Render(wrap("💭 "+thinking, width)))
		}
		if txt := m.deltas.Text(); txt != "" {
			write(lipgloss.NewStyle().Foreground(th.Ink).Render(wrap(txt, width)) + th.AccentText().Render("▍"))
		}
		if ti := m.deltas.ToolInput(); ti != "" {
			write(th.Meta().Render(wrap("⚙ "+ti, width)))
		}
	case isWorking(m.liveStatus()):
		write(th.Meta().Render("● working…"))
	}
	if u := m.tl.Usage; u.CostUSD > 0 || u.InputTokens > 0 {
		write(th.Meta().Render(fmt.Sprintf("— $%.2f · %d in · %d out —", u.CostUSD, u.InputTokens, u.OutputTokens)))
	}
	return strings.TrimRight(b.String(), "\n"), offsets
}

// isWorking reports whether the agent is busy and nothing is streaming yet.
func isWorking(st domain.SessionStatus) bool {
	return st == domain.SessionThinking || st == domain.SessionRunning || st == domain.SessionStarting
}

// renderItem draws one timeline entry; the selected one gets a left bar.
func (m *Model) renderItem(it Item, selected bool, width int) string {
	th := m.ctx.Theme
	mark := func(s string) string {
		if selected {
			return th.Selected().Render("▌") + s
		}
		return " " + s
	}
	switch it.Kind {
	case ItemStatus:
		return mark(th.Meta().Render("── " + statusLabel(it.Status) + " ──"))
	case ItemUser:
		return mark(th.AccentText().Render("you ") + lipgloss.NewStyle().Foreground(th.Ink).Render(wrap(it.Text, width-5)))
	case ItemAssistant:
		return mark(th.Markdown(it.Text, width-2))
	case ItemTool:
		return mark(m.renderTool(it, width))
	case ItemApproval:
		return mark(m.renderApproval(it))
	case ItemResolved:
		text := it.Text
		if it.Expired {
			text = "expired: " + text
		}
		return mark(th.Meta().Render("↳ " + text))
	case ItemAutoRun:
		if it.Enabled {
			return mark(th.Meta().Render("── auto-run on: tools run without asking ──"))
		}
		return mark(th.Meta().Render("── auto-run off: tools ask first ──"))
	case ItemError:
		return mark(th.Error().Render("✖ " + wrap(it.Text, width-4)))
	case ItemEnded:
		return mark(th.Meta().Render("── session " + string(it.Status) + " ──"))
	}
	return ""
}

// renderTool shows a tool call collapsed to one line, or expanded with its
// input and result.
func (m *Model) renderTool(it Item, width int) string {
	th := m.ctx.Theme
	head := th.Subtitle().Render("⚙ "+it.ToolName) + " " + th.Meta().Render(it.Summary)
	if !m.expanded[it.Seq] {
		if it.Result != "" {
			head += th.Meta().Render("  → " + truncateLine(it.Result, 40))
		}
		return head
	}
	body := []string{head}
	if len(it.Input) > 0 {
		body = append(body, th.Meta().Render(indent(prettyJSON(it.Input), "    ")))
	}
	if it.Result != "" {
		body = append(body, indent(wrap(it.Result, width-6), "    "))
	}
	return strings.Join(body, "\n")
}

// renderApproval shows a request with whether it is still pending.
func (m *Model) renderApproval(it Item) string {
	th := m.ctx.Theme
	state := th.Meta().Render("decided")
	if m.tl.isPending(it.ReqID) {
		state = th.Error().Render("pending")
	}
	accent := lipgloss.NewStyle().Foreground(th.StatusColor(rollup.Review))
	if it.IsQuestion() {
		return accent.Render("? question ") + th.Meta().Render(fmt.Sprintf("%d to answer", len(it.Questions))) + " " + state
	}
	return accent.Render("⚠ "+it.ToolName+" ") + th.Meta().Render(it.Summary) + " " + state
}

func statusLabel(st domain.SessionStatus) string {
	switch st {
	case domain.SessionIdle:
		return "your turn"
	case domain.SessionWaitingApproval:
		return "waiting approval"
	case "":
		return "unknown"
	}
	return string(st)
}
