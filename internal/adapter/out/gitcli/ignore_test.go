package gitcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIgnoredPathsAnswersForManyPathsInOneCall(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitInit(t, dir, "")
	for p, body := range map[string]string{
		".gitignore":          "node_modules/\n*.log\ndist\n",
		"node_modules/a/b.js": "x",
		"src/main.go":         "x",
		"src/node_modules/c":  "x",
		"debug.log":           "x",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewCode().IgnoredPaths(context.Background(), dir, []string{"node_modules", "src", "src/node_modules", "debug.log", "dist"})
	if err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]bool{"node_modules": true, "src": false, "src/node_modules": true, "debug.log": true, "dist": true} {
		if got[p] != want {
			t.Errorf("IgnoredPaths[%q] = %v, want %v", p, got[p], want)
		}
	}
	none, err := NewCode().IgnoredPaths(context.Background(), dir, []string{"src"})
	if err != nil || len(none) != 0 {
		t.Errorf("nothing ignored: %v, %v", none, err)
	}
}
