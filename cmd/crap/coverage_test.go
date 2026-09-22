package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoCoverFunc(t *testing.T) {
	out := []byte("operators-mcp/cmd/crap/main.go:36:\trun\t\t80.0%\n" +
		"operators-mcp/cmd/crap/coverage.go:75:\t(CoverageIndex).lookupGo\t100.0%\n" +
		"total:\t\t\t\t\t\t\t(statements)\t83.3%\n")

	entries := parseGoCoverFunc(out)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].path != "operators-mcp/cmd/crap/main.go" || entries[0].line != 36 || entries[0].percent != 80.0 {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].percent != 100.0 {
		t.Errorf("entries[1].percent = %v, want 100.0", entries[1].percent)
	}
}

func TestLoadLCOV(t *testing.T) {
	dir := t.TempDir()
	lcovPath := filepath.Join(dir, "coverage.lcov")
	content := `SF:src/foo.js
FN:1,foo
DA:1,3
DA:2,3
DA:3,0
FNF:1
FNH:1
end_of_record
SF:src/bar.js
DA:1,0
end_of_record
`
	if err := os.WriteFile(lcovPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	records, err := loadLCOV(lcovPath)
	if err != nil {
		t.Fatalf("loadLCOV: %v", err)
	}
	foo := records["src/foo.js"]
	if len(foo) != 3 {
		t.Fatalf("src/foo.js records = %v, want 3 entries", foo)
	}
	if foo[0].line != 1 || foo[0].hits != 3 {
		t.Errorf("foo[0] = %+v", foo[0])
	}
	if foo[2].hits != 0 {
		t.Errorf("foo[2].hits = %d, want 0", foo[2].hits)
	}
	if len(records["src/bar.js"]) != 1 {
		t.Errorf("src/bar.js records = %v, want 1 entry", records["src/bar.js"])
	}
}

func TestCoverageIndex_LookupGo(t *testing.T) {
	idx := &CoverageIndex{
		goCov: []goFuncCov{
			{path: "operators-mcp/internal/foo/bar.go", line: 12, percent: 75.0},
		},
	}
	fn := FunctionInfo{File: "internal/foo/bar.go", Name: "Bar", StartLine: 12, EndLine: 20}

	pct, ok := idx.Lookup(fn)
	if !ok {
		t.Fatalf("expected coverage to be found")
	}
	if pct != 0.75 {
		t.Errorf("pct = %v, want 0.75", pct)
	}

	missing := FunctionInfo{File: "internal/foo/bar.go", Name: "Missing", StartLine: 99, EndLine: 100}
	if _, ok := idx.Lookup(missing); ok {
		t.Errorf("expected no coverage for unmatched line")
	}
}

func TestCoverageIndex_LookupLCOV(t *testing.T) {
	idx := &CoverageIndex{
		lcov: map[string][]daRecord{
			"src/widget.ts": {
				{line: 10, hits: 5},
				{line: 11, hits: 0},
				{line: 12, hits: 5},
				{line: 20, hits: 0}, // outside the function range, must not count
			},
		},
	}
	fn := FunctionInfo{File: "src/widget.ts", Name: "render", StartLine: 10, EndLine: 12}

	pct, ok := idx.Lookup(fn)
	if !ok {
		t.Fatalf("expected coverage to be found")
	}
	want := 2.0 / 3.0
	if pct != want {
		t.Errorf("pct = %v, want %v", pct, want)
	}
}

func TestPathsMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"internal/foo/bar.go", "internal/foo/bar.go", true},
		{"operators-mcp/internal/foo/bar.go", "internal/foo/bar.go", true},
		{"internal/foo/bar.go", "operators-mcp/internal/foo/bar.go", true},
		{"internal/foo/bar.go", "internal/other/bar.go", false},
		{"pkg/bar.go", "internal/foo/bar.go", false},
	}
	for _, c := range cases {
		if got := pathsMatch(c.a, c.b); got != c.want {
			t.Errorf("pathsMatch(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
