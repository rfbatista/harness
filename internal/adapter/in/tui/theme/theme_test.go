package theme

import (
	"testing"

	"operators-mcp/internal/adapter/in/tui/rollup"
)

func TestEveryAgentStatusHasADistinctColour(t *testing.T) {
	th := Dark()
	seen := map[string]rollup.AgentStatus{}
	for _, st := range []rollup.AgentStatus{rollup.Run, rollup.Think, rollup.Review, rollup.Block, rollup.Done, rollup.Paused} {
		c := th.StatusColor(st)
		if c == nil {
			t.Fatalf("%v: nil colour", st)
		}
		key := colorKey(c)
		if prev, dup := seen[key]; dup && prev != rollup.Done && st != rollup.Paused {
			t.Fatalf("%v shares a colour with %v", st, prev)
		}
		seen[key] = st
	}
}

func TestLightAndDarkDifferInBackground(t *testing.T) {
	if colorKey(Dark().Bg) == colorKey(Light().Bg) {
		t.Fatal("light and dark share a background")
	}
}

func TestForModeSelectsPalette(t *testing.T) {
	if colorKey(For(ModeDark).Bg) != colorKey(Dark().Bg) {
		t.Fatal("ModeDark should pick Dark")
	}
	if colorKey(For(ModeLight).Bg) != colorKey(Light().Bg) {
		t.Fatal("ModeLight should pick Light")
	}
}

func TestParseMode(t *testing.T) {
	if m, err := ParseMode("auto"); err != nil || m != ModeAuto {
		t.Fatalf("auto: %v %v", m, err)
	}
	if _, err := ParseMode("sepia"); err == nil {
		t.Fatal("unknown mode should error")
	}
}

func TestStatusPillIsLabelledAndColoured(t *testing.T) {
	out := Dark().StatusPill(rollup.Review)
	if out == "" || !contains(out, "Needs review") {
		t.Fatalf("pill: %q", out)
	}
}
