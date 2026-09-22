package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// goFuncCov is one function-level coverage entry as reported by
// `go tool cover -func`.
type goFuncCov struct {
	path    string
	line    int
	percent float64 // 0..100
}

// daRecord is one LCOV "DA:<line>,<hits>" line-coverage entry.
type daRecord struct {
	line int
	hits int
}

// CoverageIndex holds coverage data loaded from a Go coverage profile and/or
// an LCOV file, and answers per-function coverage lookups against the
// FunctionInfo entries produced by AnalyzeFile.
type CoverageIndex struct {
	goCov []goFuncCov
	lcov  map[string][]daRecord // keyed by the SF: path as written in the LCOV file
}

// NewCoverageIndex loads whichever of goProfile / lcovPath are non-empty.
// Either or both may be omitted (empty string).
func NewCoverageIndex(goProfile, lcovPath string) (*CoverageIndex, error) {
	idx := &CoverageIndex{lcov: map[string][]daRecord{}}

	if goProfile != "" {
		entries, err := loadGoCoverage(goProfile)
		if err != nil {
			return nil, err
		}
		idx.goCov = entries
	}
	if lcovPath != "" {
		recs, err := loadLCOV(lcovPath)
		if err != nil {
			return nil, err
		}
		idx.lcov = recs
	}
	return idx, nil
}

// Lookup returns the fraction of fn covered, in [0,1], and whether any
// coverage data was found for it. Callers should treat "not found" as
// "unmeasured", not as 0% — the CRAP score still defaults unmeasured
// functions to 0% coverage, but the distinction is worth surfacing.
func (c *CoverageIndex) Lookup(fn FunctionInfo) (float64, bool) {
	if c == nil {
		return 0, false
	}
	if pct, ok := c.lookupGo(fn); ok {
		return pct, true
	}
	if pct, ok := c.lookupLCOV(fn); ok {
		return pct, true
	}
	return 0, false
}

func (c *CoverageIndex) lookupGo(fn FunctionInfo) (float64, bool) {
	base := filepath.Base(fn.File)
	var best *goFuncCov
	for i := range c.goCov {
		e := &c.goCov[i]
		if e.line != fn.StartLine || filepath.Base(e.path) != base {
			continue
		}
		if best == nil {
			best = e
		}
		if pathsMatch(e.path, fn.File) {
			best = e
			break
		}
	}
	if best == nil {
		return 0, false
	}
	return best.percent / 100.0, true
}

func (c *CoverageIndex) lookupLCOV(fn FunctionInfo) (float64, bool) {
	base := filepath.Base(fn.File)
	var records []daRecord
	found := false
	for path, recs := range c.lcov {
		if filepath.Base(path) != base {
			continue
		}
		exact := pathsMatch(path, fn.File)
		if !found || exact {
			records = recs
			found = true
		}
		if exact {
			break
		}
	}
	if !found {
		return 0, false
	}

	total, covered := 0, 0
	for _, r := range records {
		if r.line < fn.StartLine || r.line > fn.EndLine {
			continue
		}
		total++
		if r.hits > 0 {
			covered++
		}
	}
	if total == 0 {
		return 0, false
	}
	return float64(covered) / float64(total), true
}

// pathsMatch treats two paths as the same file when they're equal or one is
// a path-component suffix of the other, so a scanned relative path like
// "cmd/crap/main.go" matches a coverage tool's import-style
// "operators-mcp/cmd/crap/main.go".
func pathsMatch(a, b string) bool {
	a, b = filepath.ToSlash(a), filepath.ToSlash(b)
	if a == b {
		return true
	}
	return strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a)
}

var goCoverFuncLineRe = regexp.MustCompile(`^(\S+):(\d+):\s+\S+\s+([\d.]+)%$`)

// loadGoCoverage runs `go tool cover -func` against a coverage profile
// produced by `go test -coverprofile` and parses its per-function output.
func loadGoCoverage(profilePath string) ([]goFuncCov, error) {
	out, err := exec.Command("go", "tool", "cover", "-func="+profilePath).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go tool cover -func=%s: %w: %s", profilePath, err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("go tool cover -func=%s: %w", profilePath, err)
	}
	return parseGoCoverFunc(out), nil
}

// parseGoCoverFunc parses the per-function lines of `go tool cover -func`
// output, e.g. "operators-mcp/foo.go:12:\tFuncName\t100.0%". It silently
// skips lines that don't match, including the trailing "total:" line.
func parseGoCoverFunc(output []byte) []goFuncCov {
	var entries []goFuncCov
	for _, line := range strings.Split(string(output), "\n") {
		m := goCoverFuncLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		lineNum, errLine := strconv.Atoi(m[2])
		pct, errPct := strconv.ParseFloat(m[3], 64)
		if errLine != nil || errPct != nil {
			continue
		}
		entries = append(entries, goFuncCov{path: m[1], line: lineNum, percent: pct})
	}
	return entries
}

// loadLCOV parses an LCOV coverage file (as emitted by istanbul/nyc, jest
// --coverage, pytest-cov --cov-report=lcov, and most other language
// toolchains) into per-file line-coverage records.
func loadLCOV(path string) (map[string][]daRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open lcov file %s: %w", path, err)
	}
	defer f.Close()

	result := make(map[string][]daRecord)
	var current string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = strings.TrimPrefix(line, "SF:")
		case strings.HasPrefix(line, "DA:"):
			if current == "" {
				continue
			}
			fields := strings.SplitN(strings.TrimPrefix(line, "DA:"), ",", 2)
			if len(fields) != 2 {
				continue
			}
			lineNum, err1 := strconv.Atoi(strings.TrimSpace(fields[0]))
			hits, err2 := strconv.Atoi(strings.TrimSpace(strings.SplitN(fields[1], ",", 2)[0]))
			if err1 != nil || err2 != nil {
				continue
			}
			result[current] = append(result[current], daRecord{line: lineNum, hits: hits})
		case line == "end_of_record":
			current = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read lcov file %s: %w", path, err)
	}
	return result, nil
}
