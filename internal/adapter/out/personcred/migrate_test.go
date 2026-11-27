package personcred

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheCredentialLivesInItsOwnFolderAndAnOldFileIsMovedThereOnFirstRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".coding-pool", "person"); dir != want {
		t.Fatalf("Dir = %s, want %s (not ~/.coding-pool itself, which holds every session's worktree)", dir, want)
	}
	if p, _ := Path(); p != filepath.Join(dir, "person.cred") {
		t.Errorf("Path = %s", p)
	}

	// A credential an earlier build kept in ~/.coding-pool/person.cred is moved on first read.
	legacy := filepath.Join(home, ".coding-pool", "person.cred")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("old-cred\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (FileStore{}).Load()
	if err != nil || got != "old-cred" {
		t.Fatalf("load = %q, %v", got, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("the old file is still there: %v", err)
	}
	moved, _ := Path()
	if info, err := os.Stat(moved); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the moved credential: %v %v", info, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("its folder: %v %v", info, err)
	}
	// A second read answers it from the new place; a new file is never overwritten by an old one.
	if err := os.WriteFile(legacy, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := (FileStore{}).Load(); got != "old-cred" {
		t.Errorf("an old file replaced the credential: %q", got)
	}
}
