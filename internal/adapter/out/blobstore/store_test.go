package blobstore

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

func TestStageCommitOpenDelete(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	staged, err := s.Stage(strings.NewReader("hello world"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Size() != 11 || string(staged.Head()) != "hello world" {
		t.Fatalf("staged size %d head %q", staged.Size(), staged.Head())
	}
	if err := staged.Commit("abc"); err != nil {
		t.Fatal(err)
	}
	if p := s.Path("abc"); p != filepath.Join(dir, "abc") || !filepath.IsAbs(p) {
		t.Fatalf("Path = %q", p)
	}
	b, err := s.Open("abc")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(b.Content)
	b.Content.Close()
	if string(got) != "hello world" || b.ModTime.IsZero() {
		t.Fatalf("Open = %q, %v", got, b.ModTime)
	}
	if err := s.Delete("abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("abc"); err != nil {
		t.Fatalf("deleting a missing blob is fine: %v", err)
	}
	if _, err := s.Open("abc"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open after delete = %v", err)
	}
	assertOnly(t, dir)
}

func TestStageOverTheCapKeepsNothing(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if _, err := s.Stage(bytes.NewReader(make([]byte, 101)), 100); !errors.Is(err, ports.ErrBlobTooLarge) {
		t.Fatalf("Stage over the cap = %v", err)
	}
	staged, err := s.Stage(bytes.NewReader(make([]byte, 100)), 100)
	if err != nil {
		t.Fatalf("exactly the cap is fine: %v", err)
	}
	if len(staged.Head()) != 512 && len(staged.Head()) != 100 {
		t.Fatalf("head = %d bytes", len(staged.Head()))
	}
	staged.Discard()
	assertOnly(t, dir)
}

func TestHeadIsAtMost512Bytes(t *testing.T) {
	s := New(t.TempDir())
	staged, err := s.Stage(bytes.NewReader(make([]byte, 4096)), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Discard()
	if len(staged.Head()) != 512 {
		t.Fatalf("head = %d bytes", len(staged.Head()))
	}
}

func TestIDsCannotEscapeTheDirectory(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "store"))
	staged, err := s.Stage(strings.NewReader("x"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := staged.Commit("../escape"); err == nil {
		t.Fatal("an id with a path separator must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the blob escaped its directory")
	}
}

// assertOnly fails unless dir holds nothing (no staged leftovers).
func assertOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Fatalf("dir holds %v, want %v", got, names)
	}
}
