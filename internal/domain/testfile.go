package domain

import (
	"path"
	"strings"
)

// IsTestFile says whether rel, a path relative to the repository root, is a test
// file or something a test needs: what tests-fail-first lets a session write
// before a test has failed (ADR 0012). It knows the common stacks' conventions:
// Go _test.go, JS/TS .test. and .spec. files and __tests__, Python test_*.py,
// *_test.py and conftest.py, Ruby _spec.rb, Java and Kotlin src/test, Rust and
// general tests/ and test/ folders, and testdata, fixtures and snapshots.
func IsTestFile(rel string) bool {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	base := path.Base(rel)
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, "_test.go"),
		strings.HasSuffix(lower, "_spec.rb"), strings.HasSuffix(lower, "_test.rb"),
		strings.HasSuffix(lower, "_test.py"), strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py"),
		lower == "conftest.py",
		strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec."),
		strings.HasSuffix(lower, "test.java") || strings.HasSuffix(lower, "tests.java") || strings.HasSuffix(lower, "test.kt") || strings.HasSuffix(lower, "tests.kt"),
		strings.HasSuffix(lower, "tests.cs") || strings.HasSuffix(lower, "test.cs"),
		strings.HasSuffix(lower, "_test.rs"), strings.HasSuffix(lower, "_test.exs"), strings.HasSuffix(lower, "_test.dart"):
		return true
	}
	for _, seg := range strings.Split(path.Dir(rel), "/") {
		switch strings.ToLower(seg) {
		case "test", "tests", "__tests__", "spec", "specs", "testdata", "fixtures", "__fixtures__", "__snapshots__", "__mocks__", "testing":
			return true
		}
	}
	return false
}
