package termhost

import (
	"slices"
	"strconv"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// mouseReporting are the DEC private modes a client needs to report the
// mouse as the program asked: tracking (normal, button-event, any-event)
// and SGR encoding.
var mouseReporting = []int{1000, 1002, 1003, 1006}

// mouseModes follows which of mouseReporting the program has set. The
// emulator keeps its modes private, so they are observed on the way in:
// the handlers watch DECSET / DECRST and RIS and return false, leaving the
// emulator to act on them as before. They run inside the emulator's Write,
// which the terminal only calls under its mu, as it reads them.
type mouseModes map[int]bool

func (m *mouseModes) track(emu *vt.SafeEmulator) {
	*m = mouseModes{}
	set := func(on bool) vt.CsiHandler {
		return func(params ansi.Params) bool {
			for _, p := range params {
				if n := p.Param(-1); slices.Contains(mouseReporting, n) {
					(*m)[n] = on
				}
			}
			return false
		}
	}
	emu.RegisterCsiHandler(ansi.Command('?', 0, 'h'), set(true))
	emu.RegisterCsiHandler(ansi.Command('?', 0, 'l'), set(false))
	emu.RegisterEscHandler('c', func() bool { // RIS resets every mode
		clear(*m)
		return false
	})
}

// restore is the DECSET sequence of every mouse mode set, in a fixed order;
// "" when none is.
func (m mouseModes) restore() string {
	var s string
	for _, n := range mouseReporting {
		if m[n] {
			s += "\x1b[?" + strconv.Itoa(n) + "h"
		}
	}
	return s
}
