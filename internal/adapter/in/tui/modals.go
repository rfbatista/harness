package tui

import (
	"fmt"
	"os"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
)

// modal is a root-level overlay that owns the keyboard while open: the quit
// prompt, the command palette and the log viewer. The root holds at most one
// (Model.modal), which is what makes them mutually exclusive.
//
// This is the State pattern: each modal handles keys with the root as its
// context and closes itself by clearing m.modal.
type modal interface {
	key(m Model, k tea.KeyPressMsg) (tea.Model, tea.Cmd)
	view(m Model, ctx core.Context) string
}

// quitPrompt asks before quitting while agents are still running.
type quitPrompt struct{ live int }

func (q quitPrompt) key(m Model, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.modal = nil
	switch k.String() {
	case "y", "Y", "enter":
		return m.quit()
	}
	return m, nil
}

func (q quitPrompt) view(m Model, ctx core.Context) string {
	text := fmt.Sprintf("%d agents are still running.\nQuit anyway? (y/N)", q.live)
	return lipgloss.Place(ctx.Width, ctx.Height, lipgloss.Center, lipgloss.Center, m.th.PanelFocused().Render(text))
}

// paletteModal is the Ctrl+K finder. A pick surfaces as PaletteChosenMsg,
// which the root turns into a NavigateMsg.
type paletteModal struct{ palette components.Palette }

func newPaletteModal(th theme.Theme, items []components.PaletteItem) paletteModal {
	return paletteModal{palette: components.NewPalette(th, items)}
}

func (p paletteModal) key(m Model, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if k.String() == "esc" {
		m.modal = nil
		return m, nil
	}
	var cmd tea.Cmd
	p.palette, cmd = p.palette.Update(k)
	m.modal = p
	return m, cmd
}

func (p paletteModal) view(_ Model, ctx core.Context) string {
	return lipgloss.Place(ctx.Width, ctx.Height, lipgloss.Center, lipgloss.Top, p.palette.View(min(ctx.Width-4, 72)))
}

// logModal scrolls the slog file, since the terminal itself shows no logs.
type logModal struct{ vp viewport.Model }

func newLogModal(path string, ctx core.Context) logModal {
	vp := viewport.New(viewport.WithWidth(ctx.Width), viewport.WithHeight(ctx.Height))
	vp.SetContent(readLog(path))
	vp.GotoBottom()
	return logModal{vp: vp}
}

func readLog(path string) string {
	if path == "" {
		return "(no log path configured)"
	}
	b, err := os.ReadFile(path)
	switch {
	case err != nil:
		return "cannot read log: " + err.Error()
	case len(b) == 0:
		return "(log is empty)"
	}
	return string(b)
}

func (l logModal) key(m Model, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q", "d":
		m.modal = nil
		return m, nil
	}
	var cmd tea.Cmd
	l.vp, cmd = l.vp.Update(k)
	m.modal = l
	return m, cmd
}

func (l logModal) view(Model, core.Context) string { return l.vp.View() }
