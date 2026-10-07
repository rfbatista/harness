package artifacts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rfbatista/harnesskit/errs"
)

func TestCopyArtifactDir_CopiesTheDirectoryOnly(t *testing.T) {
	root, dest := t.TempDir(), filepath.Join(t.TempDir(), "r1")
	outside := t.TempDir()
	writeFile(t, root, "design/card.html", "<p>card</p>")
	writeFile(t, root, "design/style.css", "p{}")
	writeFile(t, root, "design/img/logo.svg", "<svg/>")
	writeFile(t, root, "design/.GIT/config", "[core]")
	writeFile(t, root, "design/sub/.git", "gitdir: elsewhere")
	writeFile(t, root, "shared/tokens.css", ":root{}")
	writeFile(t, root, "inside.css", "ok{}")
	writeFile(t, outside, "secret.txt", "s")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "design", "leak.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := os.Symlink(filepath.Join(root, "inside.css"), filepath.Join(root, "design", "linked.css")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "shared"), filepath.Join(root, "design", "shared-dir")); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactDir(root, "design/card.html", dest, 1<<20); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"card.html": "<p>card</p>", "style.css": "p{}", "img/logo.svg": "<svg/>", "linked.css": "ok{}"} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
	for _, rel := range []string{".GIT/config", "sub/.git", "leak.txt", "shared-dir", "../shared/tokens.css"} {
		if _, err := os.Lstat(filepath.Join(dest, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s must not be in the snapshot", rel)
		}
	}
	if matches, _ := filepath.Glob(dest + ".tmp-*"); len(matches) != 0 {
		t.Errorf("temporary directories left behind: %v", matches)
	}
}

func TestCopyArtifactDir_Refusals(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "design/card.html", "0123456789")
	writeFile(t, root, "design/big.bin", "0123456789")

	parent := t.TempDir()
	dest := filepath.Join(parent, "r1")
	err := copyArtifactDir(root, "design/card.html", dest, 15)
	if errs.Code(err) != "ARTIFACT_TOO_LARGE" {
		t.Fatalf("over the cap as a whole: code %q (%v)", errs.Code(err), err)
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Fatalf("a refused snapshot must leave nothing behind: %v", entries)
	}
	if err := copyArtifactDir(root, "design/gone.html", dest, 1<<20); errs.Code(err) != "ARTIFACT_NOT_FOUND" {
		t.Fatalf("missing artifact file: code %q (%v)", errs.Code(err), err)
	}
	if err := copyArtifactDir(filepath.Join(root, "nope"), "design/card.html", dest, 1<<20); err == nil {
		t.Fatal("a missing worktree must fail")
	}
}
