package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

func TestConfirmYesEmitsConfirmedWithTag(t *testing.T) {
	c := NewConfirm(theme.Dark(), "Delete agent", "Delete reviewer? This cannot be undone.", "delete:a1")
	_, cmd := c.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("y should emit")
	}
	msg, ok := cmd().(ConfirmedMsg)
	if !ok || msg.Tag != "delete:a1" {
		t.Fatalf("want ConfirmedMsg delete:a1 got %#v", cmd())
	}
}

func TestConfirmNoAndEscCancel(t *testing.T) {
	c := NewConfirm(theme.Dark(), "Delete", "sure?", "x")
	for _, k := range []tea.KeyPressMsg{{Code: 'n', Text: "n"}, {Code: tea.KeyEscape}} {
		_, cmd := c.Update(k)
		if cmd == nil {
			t.Fatal("should emit cancel")
		}
		if _, ok := cmd().(ConfirmCancelledMsg); !ok {
			t.Fatalf("want ConfirmCancelledMsg got %#v", cmd())
		}
	}
	if !contains(c.View(50), "sure?") || !contains(c.View(50), "y") {
		t.Fatalf("view:\n%s", c.View(50))
	}
}
