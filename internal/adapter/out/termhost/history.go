package termhost

import "strings"

// history is a terminal's main-screen scrollback as rendered lines, oldest
// first, bounded at max. Rendered strings cost a few hundred bytes a line
// where the emulator's cells cost over a hundred bytes a column, and pushing
// into the ring is O(1) where the emulator's buffer copies itself per line
// once full.
type history struct {
	lines []string // ring storage; grows to max, then wraps
	start int      // index of the oldest line once the ring has wrapped
	max   int
}

func newHistory(max int) *history { return &history{max: max} }

func (h *history) push(line string) {
	if len(h.lines) < h.max {
		h.lines = append(h.lines, line)
		return
	}
	h.lines[h.start] = line
	h.start = (h.start + 1) % h.max
}

func (h *history) len() int { return len(h.lines) }

// snapshot copies the lines out, oldest first. Strings are immutable, so the
// copy can be rendered after the caller unlocks.
func (h *history) snapshot() []string {
	out := make([]string, 0, len(h.lines))
	out = append(out, h.lines[h.start:]...)
	return append(out, h.lines[:h.start]...)
}

// render joins lines the way uv.Lines.Render does: "\n" between rows.
func render(lines []string) string { return strings.Join(lines, "\n") }
