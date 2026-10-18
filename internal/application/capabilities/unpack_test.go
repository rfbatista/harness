package capabilities

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// With only, the entry is matched however the manifest or the archive
// spells it: "./skills/caveman/SKILL.md" is the archive's
// "skills//caveman/SKILL.md".
func TestUnpackOnlyMatchesTheEntryCleaned(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bundle.tar.gz")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"skills//caveman/SKILL.md": "Say less.", "README.md": "readme"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}

	dest := filepath.Join(dir, "out")
	rel, err := unpack(src, dest, "./skills/caveman/SKILL.md", true)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, rel)); err != nil || string(b) != "Say less." || rel != "SKILL.md" {
		t.Errorf("unpacked %q = %q (%v), want SKILL.md = Say less.", rel, b, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); !os.IsNotExist(err) {
		t.Errorf("README.md unpacked (%v), want only the entry", err)
	}
}
