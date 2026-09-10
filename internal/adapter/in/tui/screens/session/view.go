package session

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/rollup"
)

// View composes header, feed viewport and the intervene bar.
func (m Model) View() string {
	body := lipgloss.JoinVertical(lipgloss.Left, m.header(), "", m.vp.View(), m.barView())
	if m.confirm != nil {
		return components.OverlayOn(body, m.confirm.View(min(m.ctx.Width-4, 70)), m.ctx.Width, m.ctx.Height)
	}
	return body
}

// header shows repo › branch, the status pill, elapsed · tokens · cost ·
// auto-run, any banner, and the task on a second line.
func (m Model) header() string {
	th := m.ctx.Theme
	repo := ""
	if m.sess != nil {
		for _, r := range m.snap.Repositories {
			if r.ID == m.sess.RepositoryID {
				repo = r.Name
			}
		}
	}
	crumbs := th.Subtitle().Render(repo)
	if m.sess != nil && m.sess.Branch != "" {
		crumbs += th.Meta().Render(" › ") + th.Title().Render(m.sess.Branch)
	}
	st := m.liveStatus()
	pill := lipgloss.NewStyle().Foreground(th.StatusColor(rollup.AgentStatusOf(st))).Render("● " + statusLabel(st))
	meta := ""
	if m.sess != nil {
		elapsed := time.Since(m.sess.CreatedAt).Round(time.Minute)
		if m.sess.CreatedAt.IsZero() {
			elapsed = 0
		}
		auto := "⚡ off"
		if m.sess.AutoRun {
			auto = th.AccentText().Render("⚡ auto-run")
		}
		meta = th.Meta().Render(fmt.Sprintf("· %s · %d tok · $%.2f · ", elapsed, m.sess.InputTokens+m.sess.OutputTokens, m.sess.CostUSD)) + auto
	}
	task := ""
	if m.sess != nil {
		task = th.Subtitle().Render(truncateLine(m.sess.Task, m.ctx.Width/2))
	}
	line := crumbs + "  " + pill + "  " + meta
	if m.banner != "" {
		line += "  " + th.Error().Render(m.banner)
	}
	return lipgloss.JoinVertical(lipgloss.Left, line, task)
}
