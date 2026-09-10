package components

import (
	"os"
	"strings"
	"testing"
)

func TestEditorCommandFallsBackToVi(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	if got := editorCommand(); got != "vi" {
		t.Fatalf("fallback: %q", got)
	}
	t.Setenv("EDITOR", "nvim -u NONE")
	if got := editorCommand(); got != "nvim -u NONE" {
		t.Fatalf("EDITOR: %q", got)
	}
}

func TestEditSessionRoundTrip(t *testing.T) {
	s, err := newEditSession("hello", ".md")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	defer s.cleanup()
	if !strings.HasSuffix(s.path, ".md") {
		t.Fatalf("extension: %s", s.path)
	}
	if err := os.WriteFile(s.path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := s.collect("body", nil)
	if msg.Err != nil || msg.Content != "changed" || msg.Tag != "body" {
		t.Fatalf("collect: %+v", msg)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("collect should remove the temp file")
	}
}

func TestEditSessionReportsEditorFailure(t *testing.T) {
	s, _ := newEditSession("x", ".txt")
	msg := s.collect("t", os.ErrPermission)
	if msg.Err == nil || msg.Content != "" {
		t.Fatalf("editor failure should surface: %+v", msg)
	}
}
