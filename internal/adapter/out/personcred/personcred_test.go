package personcred

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestACredentialIsKeptInAFileOnlyItsOwnerReadsAndNothingStoredIsReportedAsNone(t *testing.T) {
	dir := t.TempDir()
	s := FileStore{PathFn: func() (string, error) { return filepath.Join(dir, "state", "person.cred"), nil }}
	if _, err := s.Load(); !errors.Is(err, ErrNone) {
		t.Fatalf("an empty store: %v", err)
	}
	if err := s.Save("abc123\n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got != "abc123" {
		t.Fatalf("load = %q, %v", got, err)
	}
	p, _ := s.PathFn()
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the credential file is %v, want 0600: %v", info.Mode(), err)
	}
	if dir, err := os.Stat(filepath.Dir(p)); err != nil || dir.Mode().Perm() != 0o700 {
		t.Errorf("its folder is %v, want 0700", dir.Mode())
	}
	if err := s.Save("def"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Load(); got != "def" {
		t.Errorf("a second save left %q", got)
	}
}
