package artifacts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rfbatista/harnesskit/errs"
)

func writeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveInWorktree(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, root, "design/card.html", "<h1>hi</h1>")
	writeFile(t, root, ".git/config", "[core]")
	writeFile(t, outside, "secret.txt", "s")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "design", "leak.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	rootReal, _ := filepath.EvalSymlinks(root)

	for name, tc := range map[string]struct {
		in   string
		rel  string // expected when code == ""
		code string
	}{
		"relative":                    {"design/card.html", "design/card.html", ""},
		"dot slash":                   {"./design/card.html", "design/card.html", ""},
		"absolute, as given":          {filepath.Join(root, "design", "card.html"), "design/card.html", ""},
		"absolute, symlinks resolved": {filepath.Join(rootReal, "design", "card.html"), "design/card.html", ""},
		"missing":                     {"design/nope.html", "", "ARTIFACT_NOT_FOUND"},
		"escapes and missing":         {"../../etc/nope", "", "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
		"escapes via absolute":        {filepath.Join(outside, "secret.txt"), "", "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
		"symlink out":                 {"design/leak.txt", "", "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
		"git internals":               {".git/config", "", "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
	} {
		t.Run(name, func(t *testing.T) {
			abs, rel, err := resolveInWorktree(root, tc.in)
			if got := errs.Code(err); got != tc.code {
				t.Fatalf("code = %q (%v), want %q", got, err, tc.code)
			}
			if tc.code == "" && (rel != tc.rel || !filepath.IsAbs(abs)) {
				t.Fatalf("abs %q rel %q, want rel %q", abs, rel, tc.rel)
			}
		})
	}
}
