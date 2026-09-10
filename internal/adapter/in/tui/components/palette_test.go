package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/theme"
)

func items() []PaletteItem {
	return []PaletteItem{
		{Kind: "task", Label: "Fix login redirect", ID: "t1"},
		{Kind: "agent", Label: "reviewer", ID: "a1"},
		{Kind: "skill", Label: "tdd-workflow", ID: "k1"},
		{Kind: "project", Label: "coding_pool", ID: "p1"},
	}
}

func typeInto(p Palette, s string) Palette {
	for _, r := range s {
		p, _ = p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return p
}

func TestPaletteFiltersFuzzyAndCaseInsensitive(t *testing.T) {
	p := NewPalette(theme.Dark(), items())
	p = typeInto(p, "LOGIN")
	if got := p.Matches(); len(got) != 1 || got[0].ID != "t1" {
		t.Fatalf("want the login task, got %+v", got)
	}
	p = NewPalette(theme.Dark(), items())
	p = typeInto(p, "tdw")
	if got := p.Matches(); len(got) != 1 || got[0].ID != "k1" {
		t.Fatalf("subsequence match: %+v", got)
	}
}

func TestPaletteEnterEmitsSelection(t *testing.T) {
	p := NewPalette(theme.Dark(), items())
	p = typeInto(p, "rev")
	p, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should emit a command")
	}
	msg, ok := cmd().(PaletteChosenMsg)
	if !ok || msg.Item.ID != "a1" {
		t.Fatalf("want PaletteChosenMsg for a1, got %#v", msg)
	}
	_ = p
}

func TestPaletteDownMovesCursorWithinMatches(t *testing.T) {
	p := NewPalette(theme.Dark(), items())
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // clamps at last
	if p.Cursor() != 3 {
		t.Fatalf("cursor should clamp at 3, got %d", p.Cursor())
	}
	if !contains(p.View(60), "coding_pool") {
		t.Fatalf("view should list items: %q", p.View(60))
	}
}
