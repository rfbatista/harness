package termpane

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// sendKey forwards one key press to the child.
//
// Printable input goes as text: the emulator's key encoder only emits the bare
// key code for unmodified keys, so shift+a would otherwise be dropped. Keys
// with ctrl or alt, and special keys, go through the encoder so they honour
// the child's cursor-key and keypad modes.
func sendKey(emu *vt.SafeEmulator, msg tea.KeyPressMsg) {
	if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta) == 0 {
		emu.SendText(msg.Text)
		return
	}
	emu.SendKey(uv.KeyPressEvent{
		Text:        msg.Text,
		Mod:         uv.KeyMod(msg.Mod),
		Code:        msg.Code,
		ShiftedCode: msg.ShiftedCode,
		BaseCode:    msg.BaseCode,
		IsRepeat:    msg.IsRepeat,
	})
}
