package tui

import (
	"image/color"
	"regexp"

	tea "charm.land/bubbletea/v2"
)

func lightColor() color.Color { return color.RGBA{R: 0xF4, G: 0xF2, B: 0xEC, A: 0xFF} }

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

// driveRoot runs a command chain to completion through the root model. It
// stops after a bounded number of hops so a blocking wait can never hang a
// test; none of the flows driven here arm an event wait.
func driveRoot(m Model, cmd tea.Cmd) Model {
	queue := []tea.Cmd{cmd}
	for hops := 0; len(queue) > 0 && hops < 64; hops++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if msg == nil {
			continue
		}
		if _, quit := msg.(tea.QuitMsg); quit {
			continue
		}
		next, cmd := m.Update(msg)
		m = next.(Model)
		queue = append(queue, cmd)
	}
	return m
}
