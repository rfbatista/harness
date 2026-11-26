package contextmap

import (
	"fmt"
	"strings"
	"testing"
)

func lineCount(s string) int { return len(strings.Split(strings.TrimRight(s, "\n"), "\n")) }

func TestGenerateAnEmptyRepositoryIsAMapWithOnlyItsHeader(t *testing.T) {
	got := Generate(Input{Repository: "empty", Branch: "main", Commit: "c0ffee"})
	if !strings.Contains(got, "# Context map: empty") || !strings.Contains(got, "main @ c0ffee") {
		t.Errorf("header missing:\n%s", got)
	}
	for _, heading := range []string{"## Commands", "## Entry points", "## Where to look"} {
		if strings.Contains(got, heading) {
			t.Errorf("an empty repository has a %q section:\n%s", heading, got)
		}
	}
}

func TestGenerateARepositoryWithoutAGENTSPointsToNothingHandWritten(t *testing.T) {
	in := Input{Repository: "bare", Branch: "main", Commit: "abc", Files: []string{"main.go", "pkg/a.go"}}
	got := Generate(in)
	if strings.Contains(got, "hand-written") || strings.Contains(got, "AGENTS.md") {
		t.Errorf("the map points to instruction files the repository lacks:\n%s", got)
	}
	if !strings.Contains(got, "## Entry points") || !strings.Contains(got, "- `main.go`") {
		t.Errorf("entry points lost:\n%s", got)
	}
}

func TestGenerateNeverFailsOnABinaryOrBrokenPackageJSON(t *testing.T) {
	in := Input{
		Repository: "r", Branch: "main", Commit: "abc",
		Files: []string{"a/package.json", "b/package.json", "c/package.json", "logo.png"},
		Read: func(p string) ([]byte, bool) {
			switch p {
			case "a/package.json":
				return []byte{0x89, 'P', 'N', 'G', 0, 0, 0xff}, true
			case "b/package.json":
				return []byte(`{"main": 42}`), true
			}
			return nil, false
		},
	}
	got := Generate(in)
	if strings.Contains(got, "## Entry points") {
		t.Errorf("a package.json without a usable main is an entry point:\n%s", got)
	}
	if !strings.Contains(got, "`a/`") {
		t.Errorf("layout lacks a/:\n%s", got)
	}
}

func TestGenerateAHugeVendoredTreeStaysWithinTheCap(t *testing.T) {
	in := Input{Repository: "r", Branch: "main", Commit: "abc", Gates: []Gate{{Kind: "test", Command: "make test"}}, Instructions: []string{"AGENTS.md"}}
	for i := 0; i < 2000; i++ {
		in.Files = append(in.Files, fmt.Sprintf("node_modules/pkg%04d/lib/index.js", i), fmt.Sprintf("vendor/mod%04d/x.go", i))
	}
	got := Generate(in)
	if n := lineCount(got); n > MaxLines {
		t.Errorf("map has %d lines, cap %d", n, MaxLines)
	}
	if !strings.Contains(got, "- test: `make test`") || !strings.Contains(got, "AGENTS.md") {
		t.Errorf("the cap cut commands or pointers")
	}
}

// Entry points are "never cut" (Generate's doc), yet the map has a hard cap
// of 300 lines. A monorepo with many main packages must still honour the cap.
func TestGenerateManyEntryPointsStillHonourTheLineCap(t *testing.T) {
	in := Input{Repository: "mono", Branch: "main", Commit: "abc"}
	for i := 0; i < 400; i++ {
		in.Files = append(in.Files, fmt.Sprintf("services/s%03d/cmd/main.go", i))
	}
	if n := lineCount(Generate(in)); n > MaxLines {
		t.Errorf("map has %d lines, cap %d (entry points are not counted against the cap)", n, MaxLines)
	}
}

// A vendored or dependency directory is not the repository's own layout: its
// main.go and package.json mains are not entry points to read first.
func TestGenerateVendoredDirectoriesAreNotEntryPoints(t *testing.T) {
	in := Input{
		Repository: "r", Branch: "main", Commit: "abc",
		Files: []string{"cmd/app/main.go", "vendor/github.com/x/tool/main.go", "node_modules/lib/package.json"},
		Read: func(p string) ([]byte, bool) {
			if p == "node_modules/lib/package.json" {
				return []byte(`{"main":"index.js"}`), true
			}
			return nil, false
		},
	}
	got := Generate(in)
	if !strings.Contains(got, "- `cmd/app/main.go`") {
		t.Fatalf("the repository's own entry point is missing:\n%s", got)
	}
	for _, bad := range []string{"vendor/github.com/x/tool/main.go", "node_modules/lib/index.js"} {
		if strings.Contains(got, "- `"+bad+"`") {
			t.Errorf("vendored entry point %q listed:\n%s", bad, got)
		}
	}
}

func TestGenerateStaysUnderTheCapWhenOnlyTheLayoutBudgetIsTiny(t *testing.T) {
	in := Input{Repository: "r", Branch: "main", Commit: "abc"}
	for i := 0; i < 250; i++ {
		in.Gates = append(in.Gates, Gate{Kind: "lint", Command: fmt.Sprintf("lint %d", i)})
	}
	for i := 0; i < 50; i++ {
		in.Files = append(in.Files, fmt.Sprintf("d%02d/f.go", i))
	}
	got := Generate(in)
	if n := lineCount(got); n > MaxLines {
		t.Errorf("map has %d lines, cap %d", n, MaxLines)
	}
	if !strings.Contains(got, "more directories") {
		t.Errorf("a layout cut to nothing does not say so:\n%s", got)
	}
}

func TestGenerateFilesAtTheRootAreNotDirectories(t *testing.T) {
	got := Generate(Input{Repository: "r", Branch: "main", Commit: "abc", Files: []string{"a.go", "b.go"}})
	if strings.Contains(got, "- `/`") || strings.Contains(got, "- `a.go/`") {
		t.Errorf("root files shown as directories:\n%s", got)
	}
}
