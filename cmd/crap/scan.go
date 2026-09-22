package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var defaultIgnoreDirs = set(".git", "vendor", "node_modules", "dist", "build", "bin", ".next", "__pycache__", ".venv", "venv")

// isTestFile reports whether path looks like a test file under the
// conventions of its language (Go's _test.go, Python's test_*.py /
// *_test.py, JS/TS's *.test.* / *.spec.*).
func isTestFile(path string) bool {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	switch ext {
	case ".go":
		return strings.HasSuffix(name, "_test")
	case ".py":
		return strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test")
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".spec")
	}
	return false
}

// ScanFiles walks paths (files or directories) and returns every supported
// source file found, deduplicated and sorted. Directories matching
// defaultIgnoreDirs, or any path matching an exclude pattern, are skipped.
// Test files are skipped unless includeTests is true.
func ScanFiles(paths []string, excludes []string, includeTests bool) ([]string, error) {
	seen := map[string]bool{}
	var files []string

	add := func(p string) {
		clean := filepath.Clean(p)
		if seen[clean] {
			return
		}
		ext := strings.ToLower(filepath.Ext(clean))
		if !SupportsExt(ext) {
			return
		}
		if !includeTests && isTestFile(clean) {
			return
		}
		if excluded(clean, excludes) {
			return
		}
		seen[clean] = true
		files = append(files, clean)
	}

	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			add(root)
			continue
		}

		err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != root && (defaultIgnoreDirs[d.Name()] || excluded(p, excludes)) {
					return filepath.SkipDir
				}
				return nil
			}
			add(p)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(files)
	return files, nil
}

// excluded reports whether path matches any user-supplied exclude pattern,
// tried as a glob against the full slash-form path, a glob against the base
// name, and finally a plain substring match for convenience (e.g. "generated").
func excluded(path string, patterns []string) bool {
	slashPath := filepath.ToSlash(path)
	base := filepath.Base(path)
	for _, pat := range patterns {
		if ok, _ := filepath.Match(pat, slashPath); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
		if strings.Contains(slashPath, pat) {
			return true
		}
	}
	return false
}
