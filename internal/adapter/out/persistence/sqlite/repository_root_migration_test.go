package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

func repositoryRootDir(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var got string
	if err := db.Raw("SELECT root_dir FROM repositories WHERE id = ?", id).Scan(&got).Error; err != nil {
		t.Fatalf("read repository %s: %v", id, err)
	}
	return got
}

// A repository root dir is stored absolute (ADR 0072). Repositories saved
// before that with a leading "~" are rewritten to the server's home when the
// database is opened; absolute and empty root dirs are left alone, so opening
// again changes nothing.
func TestOpenExpandsRepositoryRootDirsStoredWithATilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	path := filepath.Join(t.TempDir(), "data.db")
	db := openFile(t, path)
	mustExec(t, db, "INSERT INTO repositories (id, root_dir) VALUES ('tilde-path', '~/projetos/harnesskit'), ('tilde-alone', '~'), ('absolute', '/srv/app'), ('empty', ''), ('tilde-inside', '/srv/~x')")
	if err := Close(db); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"tilde-path":   filepath.Join(home, "projetos/harnesskit"),
		"tilde-alone":  home,
		"absolute":     "/srv/app",
		"empty":        "",
		"tilde-inside": "/srv/~x",
	}
	for opening := 1; opening <= 2; opening++ {
		db = openFile(t, path)
		for id, root := range want {
			if got := repositoryRootDir(t, db, id); got != root {
				t.Errorf("opening %d, repository %s: root_dir = %q, want %q", opening, id, got, root)
			}
		}
		if err := Close(db); err != nil {
			t.Fatal(err)
		}
	}
}
