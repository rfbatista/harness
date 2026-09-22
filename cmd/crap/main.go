package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const usage = `crap: standalone CRAP (Change Risk Anti-Patterns) analyzer

Computes, for every function/method found under the given paths:

    CRAP(m) = complexity(m)^2 * (1 - coverage(m))^3 + complexity(m)

Complexity is McCabe cyclomatic complexity from static analysis. Coverage
comes from a Go coverage profile and/or an LCOV file, if supplied; a
function with no coverage data is assumed to be at 0% (worst case). Without
either -cover or -lcov, every function is scored at 0% coverage, so the
score is really just complexity — pass coverage data for a meaningful signal.

Supports Go, JavaScript, TypeScript (incl. TSX) and Python.

Usage:
  crap [flags] [path ...]

Paths may be files or directories (scanned recursively); defaults to ".".
Directories matching common build/vendor conventions (.git, vendor,
node_modules, dist, build, bin, .next, __pycache__, .venv, venv) are always
skipped. Test files are skipped by default; pass -include-tests to analyze
them too.

Examples:
  # Scan everything under the current directory, sorted worst-first.
  crap

  # Score against real coverage: run tests first, then feed the profile in.
  go test -coverprofile=cover.out ./...
  crap -cover=cover.out ./internal

  # JS/TS/Python coverage comes from an LCOV file instead.
  crap -lcov=coverage/lcov.info ./src

  # Zoom in on the riskiest functions instead of scrolling the full table.
  crap -cover=cover.out -top=20 ./internal

  # Skip the noise: generated code, vendored deps, one-off scripts.
  crap -cover=cover.out -exclude='*.pb.go' -exclude=mocks ./internal

  # Machine-readable output for another tool or a report.
  crap -json -cover=cover.out ./internal > crap.json

  # CI gate: fail the build if anything crosses a risk threshold.
  # A CRAP score over ~30 is the commonly cited line for "needs attention".
  crap -cover=cover.out -fail-over=30 ./internal

Common patterns:
  - Always pair a coverage source (-cover and/or -lcov) with the scan;
    complexity alone is a much weaker signal than complexity + coverage.
  - Use -top or -min-crap to focus review/refactor effort on the highest-risk
    functions rather than reading the whole table.
  - Use -exclude repeatedly to keep generated code, vendored packages, and
    scripts out of the score instead of trying to write one pattern that
    matches everything.
  - Reach for -json when piping into another tool (dashboards, PR comments,
    trend tracking); use the table for a human reading it directly.
  - Wire -fail-over into CI once a baseline is established, rather than at
    the start — it turns the tool into a regression gate, not a one-off
    report.

Exit codes:
  0  success (including -h/-help)
  1  a function's CRAP score exceeded -fail-over
  2  usage error, or a path/scan/coverage-file error

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("crap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}

	var (
		goCover      string
		lcovPath     string
		jsonOut      bool
		minCRAP      float64
		top          int
		includeTests bool
		failOver     float64
		excludeFlag  stringList
	)
	fs.StringVar(&goCover, "cover", "", "Go coverage profile, from `go test -coverprofile=FILE`")
	fs.StringVar(&lcovPath, "lcov", "", "LCOV coverage file (JS/TS/Python via nyc, jest, coverage.py, ...)")
	fs.BoolVar(&jsonOut, "json", false, "print results as JSON instead of a table")
	fs.Float64Var(&minCRAP, "min-crap", 0, "only report functions with CRAP >= this value")
	fs.IntVar(&top, "top", 0, "only report the top N functions by CRAP score (0 = all)")
	fs.BoolVar(&includeTests, "include-tests", false, "also analyze test files (excluded by default)")
	fs.Float64Var(&failOver, "fail-over", 0, "exit with status 1 if any function's CRAP score exceeds this (0 = disabled)")
	fs.Var(&excludeFlag, "exclude", "glob or substring pattern to exclude; may be repeated")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	files, err := ScanFiles(paths, excludeFlag, includeTests)
	if err != nil {
		fmt.Fprintln(stderr, "crap:", err)
		return 2
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "crap: no supported source files found under", strings.Join(paths, ", "))
		return 2
	}

	var funcs []FunctionInfo
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(stderr, "crap:", err)
			return 2
		}
		fns, err := AnalyzeFile(f, src)
		if err != nil {
			fmt.Fprintln(stderr, "crap:", err)
			return 2
		}
		funcs = append(funcs, fns...)
	}

	cov, err := NewCoverageIndex(goCover, lcovPath)
	if err != nil {
		fmt.Fprintln(stderr, "crap:", err)
		return 2
	}

	results := BuildResults(funcs, cov)
	SortResults(results)
	results = FilterResults(results, minCRAP, top)

	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(ToReports(results)); err != nil {
			fmt.Fprintln(stderr, "crap:", err)
			return 2
		}
	} else {
		WriteTable(stdout, results)
	}

	if failOver > 0 {
		for _, r := range results {
			if r.CRAP > failOver {
				return 1
			}
		}
	}
	return 0
}

// stringList collects repeated -exclude flag occurrences.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
