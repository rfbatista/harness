package domain

import "testing"

func TestCleanRunCommand(t *testing.T) {
	name, cmd, err := CleanRunCommand("  dev server ", "  make air  ")
	if err != nil || name != "dev server" || cmd != "make air" {
		t.Fatalf("got %q %q %v", name, cmd, err)
	}
	for _, c := range []struct{ name, cmd, code string }{
		{"", "make air", "INVALID_NAME"},
		{"-x", "make air", "INVALID_NAME"},
		{"a/b", "make air", "INVALID_NAME"},
		{"server", "   ", "INVALID_INPUT"},
	} {
		if _, _, err := CleanRunCommand(c.name, c.cmd); err == nil || err.(*StructuredError).Code != c.code {
			t.Errorf("%q %q: got %v, want %s", c.name, c.cmd, err, c.code)
		}
	}
}
