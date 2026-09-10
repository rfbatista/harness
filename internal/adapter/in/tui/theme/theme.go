// Package theme holds the TUI's visual language: the palette ported from the
// Flutter app_theme.dart (status colours, surface ladder, accent) and the
// lipgloss styles built on it. Everything that draws goes through a Theme so
// light and dark terminals render the same structure.
package theme

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/rollup"
)

// Mode selects the palette: detected from the terminal, or forced by a flag.
type Mode int

const (
	ModeAuto Mode = iota
	ModeDark
	ModeLight
)

// ParseMode reads the --theme flag value.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ModeAuto, nil
	case "dark":
		return ModeDark, nil
	case "light":
		return ModeLight, nil
	}
	return ModeAuto, fmt.Errorf("theme: unknown mode %q (want auto, dark or light)", s)
}

// Theme is one resolved palette plus the styles derived from it.
type Theme struct {
	IsDark bool

	Bg, Surface, Surface2, Surface3 color.Color
	Ink, Muted, Dim                 color.Color
	Border, BorderStrong            color.Color
	Accent                          color.Color

	run, think, review, block, done color.Color
	DiffAdd, DiffDel                color.Color
}

// Dark is the near-black theme with warm off-white ink.
func Dark() Theme {
	return Theme{
		IsDark:       true,
		Bg:           lipgloss.Color("#0B0C10"),
		Surface:      lipgloss.Color("#12131A"),
		Surface2:     lipgloss.Color("#1A1C25"),
		Surface3:     lipgloss.Color("#232631"),
		Ink:          lipgloss.Color("#ECEAE2"),
		Muted:        lipgloss.Color("#A2A39B"),
		Dim:          lipgloss.Color("#6E7068"),
		Border:       lipgloss.Color("#262833"),
		BorderStrong: lipgloss.Color("#3A3D4A"),
		Accent:       lipgloss.Color("#B7F23C"),
		run:          lipgloss.Color("#B7F23C"),
		think:        lipgloss.Color("#7CC0E8"),
		review:       lipgloss.Color("#F0C04A"),
		block:        lipgloss.Color("#E5765E"),
		done:         lipgloss.Color("#6E7068"),
		DiffAdd:      lipgloss.Color("#7BD88F"),
		DiffDel:      lipgloss.Color("#E5765E"),
	}
}

// Light is the warm-paper theme.
func Light() Theme {
	return Theme{
		IsDark:       false,
		Bg:           lipgloss.Color("#F4F2EC"),
		Surface:      lipgloss.Color("#FFFFFF"),
		Surface2:     lipgloss.Color("#ECEAE2"),
		Surface3:     lipgloss.Color("#E2E0D7"),
		Ink:          lipgloss.Color("#17181C"),
		Muted:        lipgloss.Color("#5C5E58"),
		Dim:          lipgloss.Color("#8A8C84"),
		Border:       lipgloss.Color("#DDDBD2"),
		BorderStrong: lipgloss.Color("#C6C4BB"),
		Accent:       lipgloss.Color("#97D700"),
		run:          lipgloss.Color("#6FA300"),
		think:        lipgloss.Color("#2E7FB0"),
		review:       lipgloss.Color("#B98A10"),
		block:        lipgloss.Color("#C2503A"),
		done:         lipgloss.Color("#6E7068"),
		DiffAdd:      lipgloss.Color("#2F8F4E"),
		DiffDel:      lipgloss.Color("#C2503A"),
	}
}

// For resolves a mode into a theme; ModeAuto is treated as dark until the
// terminal reports its background (see Program's BackgroundColorMsg handling).
func For(m Mode) Theme {
	if m == ModeLight {
		return Light()
	}
	return Dark()
}

// StatusColor is the foreground for an agent status. Paused reuses the
// done grey on purpose: it is inert, not alarming.
func (t Theme) StatusColor(st rollup.AgentStatus) color.Color {
	switch st {
	case rollup.Run:
		return t.run
	case rollup.Think:
		return t.think
	case rollup.Review:
		return t.review
	case rollup.Block:
		return t.block
	}
	return t.done
}

// TaskStatusColor maps a task rollup onto the same five status colours.
func (t Theme) TaskStatusColor(st rollup.TaskStatus) color.Color {
	switch st {
	case rollup.Running:
		return t.run
	case rollup.NeedsReview:
		return t.review
	case rollup.Blocked:
		return t.block
	}
	return t.done
}

// StatusPill renders "● Label" in the status colour.
func (t Theme) StatusPill(st rollup.AgentStatus) string {
	return lipgloss.NewStyle().Foreground(t.StatusColor(st)).Render("● " + st.Label())
}

// TaskPill renders "● Label" for a task rollup status.
func (t Theme) TaskPill(st rollup.TaskStatus) string {
	return lipgloss.NewStyle().Foreground(t.TaskStatusColor(st)).Render("● " + st.Label())
}

// Styles used across screens. Kept as methods so a theme swap re-derives them.

func (t Theme) Title() lipgloss.Style    { return lipgloss.NewStyle().Bold(true).Foreground(t.Ink) }
func (t Theme) Subtitle() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.Muted) }
func (t Theme) Meta() lipgloss.Style     { return lipgloss.NewStyle().Foreground(t.Dim) }
func (t Theme) AccentText() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
}
func (t Theme) Selected() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Ink).Background(t.Surface3).Bold(true)
}
func (t Theme) Panel() lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border).Padding(0, 1)
}
func (t Theme) PanelFocused() lipgloss.Style {
	return t.Panel().BorderForeground(t.BorderStrong)
}
func (t Theme) Error() lipgloss.Style { return lipgloss.NewStyle().Foreground(t.block) }
func (t Theme) KeyHint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Ink).Background(t.Surface2).Padding(0, 1)
}
