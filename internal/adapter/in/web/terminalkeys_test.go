package web

import (
	"encoding/json"
	"os"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// The browser translates DOM key events into ports.KeyEvent with the codes in
// web/src/modules/sessions/infrastructure/terminal-keys.json; the server decodes them with ultraviolet.
// This pins the two together.
func TestTerminalKeyCodesMatchUltraviolet(t *testing.T) {
	raw, err := os.ReadFile("../../../../web/src/modules/sessions/infrastructure/terminal-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Mods  map[string]int  `json:"mods"`
		Codes map[string]rune `json:"codes"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}

	wantMods := map[string]uv.KeyMod{"shift": uv.ModShift, "alt": uv.ModAlt, "ctrl": uv.ModCtrl, "meta": uv.ModMeta}
	for name, want := range wantMods {
		if got := fixture.Mods[name]; uv.KeyMod(got) != want {
			t.Errorf("mod %s = %d, want %d", name, got, want)
		}
	}

	wantCodes := map[string]rune{
		"Enter": uv.KeyEnter, "Backspace": uv.KeyBackspace, "Tab": uv.KeyTab, "Escape": uv.KeyEscape,
		"ArrowUp": uv.KeyUp, "ArrowDown": uv.KeyDown, "ArrowRight": uv.KeyRight, "ArrowLeft": uv.KeyLeft,
		"Insert": uv.KeyInsert, "Delete": uv.KeyDelete, "PageUp": uv.KeyPgUp, "PageDown": uv.KeyPgDown,
		"Home": uv.KeyHome, "End": uv.KeyEnd,
		"F1": uv.KeyF1, "F2": uv.KeyF2, "F3": uv.KeyF3, "F4": uv.KeyF4, "F5": uv.KeyF5, "F6": uv.KeyF6,
		"F7": uv.KeyF7, "F8": uv.KeyF8, "F9": uv.KeyF9, "F10": uv.KeyF10, "F11": uv.KeyF11, "F12": uv.KeyF12,
	}
	if len(fixture.Codes) != len(wantCodes) {
		t.Errorf("fixture has %d codes, want %d", len(fixture.Codes), len(wantCodes))
	}
	for name, want := range wantCodes {
		if got, ok := fixture.Codes[name]; !ok || got != want {
			t.Errorf("code %s = %d, want %d", name, got, want)
		}
	}
}
