package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		"foo_test.go":    true,
		"foo.go":         false,
		"test_foo.py":    true,
		"foo_test.py":    true,
		"foo.py":         false,
		"widget.test.ts": true,
		"widget.spec.js": true,
		"widget.ts":      false,
	}
	for name, want := range cases {
		if got := isTestFile(name); got != want {
			t.Errorf("isTestFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestScanFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte("package x\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	mustWrite("main.go")
	mustWrite("main_test.go")
	mustWrite("sub/helper.go")
	mustWrite("vendor/dep/dep.go")
	mustWrite("node_modules/pkg/index.js")
	mustWrite("README.md")

	files, err := ScanFiles([]string{dir}, nil, false)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	rel := make([]string, len(files))
	for i, f := range files {
		r, err := filepath.Rel(dir, f)
		if err != nil {
			t.Fatalf("Rel: %v", err)
		}
		rel[i] = filepath.ToSlash(r)
	}

	want := []string{"main.go", "sub/helper.go"}
	if len(rel) != len(want) {
		t.Fatalf("got %v, want %v", rel, want)
	}
	for i := range want {
		if rel[i] != want[i] {
			t.Errorf("rel[%d] = %q, want %q (full: %v)", i, rel[i], want[i], rel)
		}
	}
}

func TestScanFiles_IncludeTests(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files, err := ScanFiles([]string{dir}, nil, true)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %v, want 1 file", files)
	}
}

func TestExcluded(t *testing.T) {
	if !excluded("internal/generated/foo.go", []string{"generated"}) {
		t.Errorf("expected substring exclude match")
	}
	if !excluded("internal/foo/bar.go", []string{"*/foo/*"}) {
		t.Errorf("expected glob exclude match")
	}
	if excluded("internal/foo/bar.go", []string{"baz"}) {
		t.Errorf("expected no match")
	}
}
