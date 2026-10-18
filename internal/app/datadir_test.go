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
