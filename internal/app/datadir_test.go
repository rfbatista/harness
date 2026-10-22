package app

import (
	"path/filepath"
	"testing"
)

// Token savers run from tools/ in another working directory (a checkout, a
// worktree), so a relative data dir would point nowhere there.
func TestDataDirIsAbsoluteForARelativeDatabasePath(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, db := range []string{"data.db", "var/data.db"} {
		dir, err := dataDir(Config{DBPath: db}, "tools")
		if err != nil {
			t.Fatalf("dataDir(%q): %v", db, err)
		}
		want, _ := filepath.Abs(filepath.Join(filepath.Dir(db), "tools"))
		if dir != want {
			t.Errorf("dataDir(%q) = %q, want %q", db, dir, want)
		}
	}
}

// A named data dir holds the data wherever the database is.
func TestDataDirIsTheNamedOneWhateverTheDatabase(t *testing.T) {
	root := t.TempDir()
	for _, db := range []string{":memory:", "elsewhere/data.db"} {
		cfg := Config{DBPath: db, DataDir: root}
		if dir, err := dataDir(cfg, "tools"); err != nil || dir != filepath.Join(root, "tools") {
			t.Errorf("dataDir with db %q = %q (%v), want %q", db, dir, err, filepath.Join(root, "tools"))
		}
		if dir, err := secretsDir(cfg); err != nil || dir != root {
			t.Errorf("secretsDir with db %q = %q (%v), want %q", db, dir, err, root)
		}
	}
}
