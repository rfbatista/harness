// Package ui holds what every tui-client screen draws with: the styles, list
// windowing and cursor keys, and how an error reads to a person.
package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"operators-mcp/internal/domain"
)

var (
	Header = lipgloss.NewStyle().Bold(true)
	Crumb  = lipgloss.NewStyle().Faint(true)
	Error  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	Note   = lipgloss.NewStyle().Faint(true)
	Cursor = lipgloss.NewStyle().Reverse(true)
	Live   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	Dim    = lipgloss.NewStyle().Faint(true)
)

// Clamp keeps i inside a list of n items.
func Clamp(i, n int) int {
	if n == 0 {
		return 0
	}
	return max(0, min(i, n-1))
}

// Fit pads or truncates s to exactly w cells.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// Window returns the [from, to) slice of n rows to show so cursor stays
// visible in height rows.
func Window(cursor, n, height int) (int, int) {
	if n <= height {
		return 0, n
	}
	from := max(0, min(cursor-height/2, n-height))
	return from, from + height
}

// MoveCursor applies the list navigation keys; ok is false for other keys.
func MoveCursor(k string, cursor, n int) (int, bool) {
	switch k {
	case "up", "k":
		return Clamp(cursor-1, n), true
	case "down", "j":
		return Clamp(cursor+1, n), true
	case "home", "g":
		return 0, true
	case "end", "G":
		return Clamp(n-1, n), true
	}
	return cursor, false
}

// Row highlights a list row across the full width when it is selected.
func Row(row string, selected bool, width int) string {
	if selected {
		return Cursor.Render(Fit(row, width))
	}
	return row
}

// Plural is "s" unless n is one.
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// StatusLabel is a ticket status as people read it.
func StatusLabel(s domain.TicketStatus) string {
	return strings.ReplaceAll(string(s), "_", " ")
}

// Age is how long ago t was, coarsely.
func Age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// Describe is err as the header shows it: a domain error reads as what its
// code means to the user, anything else as its message.
func Describe(err error) string {
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		return err.Error()
	}
	switch se.Code {
	case "SESSION_TRANSCRIPT_MISSING":
		return "claude has no saved conversation for that session any more"
	case "WORKSPACE_MISSING":
		return "that session's worktree was removed; it cannot be resumed"
	case "BRANCH_EXISTS":
		return "a branch for this session already exists; try again"
	}
	return se.Message
}
