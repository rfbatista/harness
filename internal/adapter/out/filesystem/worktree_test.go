package filesystem_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/domain"
)

func worktree(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "wt")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "dir/sub"), 0o755))
	must(os.WriteFile(filepath.Join(root, "dir/sub/a.go"), []byte("package a\n"), 0o644))
	must(os.WriteFile(filepath.Join(base, "secret"), []byte("outside\n"), 0o644))
	must(os.Symlink("dir", filepath.Join(root, "linkdir")))
	must(os.Symlink("../secret", filepath.Join(root, "out")))
	must(os.Symlink(base, filepath.Join(root, "outdir")))
	must(os.Symlink("dir/sub/a.go", filepath.Join(root, "in")))
	return root
}

func TestWorktreeFilesStat(t *testing.T) {
	root := worktree(t)
	w := filesystem.NewWorktreeFiles()
	st, err := w.Stat(root, "dir/sub/a.go")
	if err != nil || st.Kind != domain.EntryFile || st.Size != 10 {
		t.Fatalf("a.go = %+v, %v", st, err)
	}
	if st, _ := w.Stat(root, "dir"); st.Kind != domain.EntryDir {
		t.Errorf("dir = %+v", st)
	}
	if st, _ := w.Stat(root, "in"); st.Kind != domain.EntrySymlink || st.Target != "dir/sub/a.go" || st.Escapes {
		t.Errorf("in = %+v", st)
	}
	if st, _ := w.Stat(root, "out"); st.Kind != domain.EntrySymlink || !st.Escapes {
		t.Errorf("out = %+v", st)
	}
	for p, code := range map[string]string{
		"missing": "PATH_NOT_FOUND", "dir/sub/a.go/x": "PATH_NOT_FOUND",
		"linkdir/sub/a.go": "PATH_NOT_FOUND", // never through a link
		"outdir/secret":    "INVALID_INPUT",  // a link out of the root
	} {
		if _, err := w.Stat(root, p); errs.Code(err) != code {
			t.Errorf("Stat(%q) = %v, want %s", p, err, code)
		}
	}
	if _, err := w.Stat(filepath.Join(root, "gone"), "x"); errs.Code(err) != "WORKSPACE_MISSING" {
		t.Errorf("Stat in a missing worktree = %v, want WORKSPACE_MISSING", err)
	}
}

func TestWorktreeFilesReadNeverLeavesTheRoot(t *testing.T) {
	root := worktree(t)
	w := filesystem.NewWorktreeFiles()
	data, size, err := w.Read(root, "dir/sub/a.go", 4)
	if err != nil || string(data) != "pack" || size != 10 {
		t.Fatalf("Read = %q, %d, %v", data, size, err)
	}
	for p, code := range map[string]string{"out": "INVALID_INPUT", "in": "PATH_NOT_FOUND", "dir": "PATH_NOT_FOUND", "outdir/secret": "INVALID_INPUT"} {
		if _, _, err := w.Read(root, p, 100); errs.Code(err) != code {
			t.Errorf("Read(%q) = %v, want %s", p, err, code)
		}
	}
}

func TestWorktreeFilesLines(t *testing.T) {
	root := t.TempDir()
	w := filesystem.NewWorktreeFiles()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A multibyte rune straddles the 64 KiB read boundary.
	long := strings.Repeat("a", 64<<10-1) + "é\n" + strings.Repeat("line\n", 3) + "end"
	write("long.txt", long)
	write("bin", "x\x00y\n")
	write("bad", "ok\n\xff\n")
	write("empty", "")
	for name, want := range map[string][2]any{"long.txt": {5, false}, "bin": {1, true}, "bad": {2, true}, "empty": {0, false}} {
		n, binary, err := w.Lines(root, name)
		if err != nil || n != want[0].(int) || binary != want[1].(bool) {
			t.Errorf("Lines(%s) = %d, %v, %v; want %v", name, n, binary, err, want)
		}
	}
}
