package main

import (
	"fmt"
	"io"
	"math"
	"sort"
	"text/tabwriter"
)

// Result is one function's complexity, coverage and derived CRAP score.
type Result struct {
	FunctionInfo
	Coverage      float64 // fraction in [0,1]; 0 when unmeasured
	CoverageKnown bool
	CRAP          float64
}

// ComputeCRAP implements the standard CRAP formula:
// CRAP(m) = complexity(m)^2 * (1 - coverage(m))^3 + complexity(m)
// where coverage is a fraction in [0,1].
func ComputeCRAP(complexity int, coverage float64) float64 {
	c := float64(complexity)
	return c*c*math.Pow(1-coverage, 3) + c
}

// BuildResults attaches coverage data (when available) to each function and
// computes its CRAP score.
func BuildResults(funcs []FunctionInfo, cov *CoverageIndex) []Result {
	results := make([]Result, 0, len(funcs))
	for _, fn := range funcs {
		pct, known := cov.Lookup(fn)
		results = append(results, Result{
			FunctionInfo:  fn,
			Coverage:      pct,
			CoverageKnown: known,
			CRAP:          ComputeCRAP(fn.Complexity, pct),
		})
	}
	return results
}

// SortResults orders results by descending CRAP score, breaking ties by
// file and line so output is deterministic.
func SortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].CRAP != results[j].CRAP {
			return results[i].CRAP > results[j].CRAP
		}
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		return results[i].StartLine < results[j].StartLine
	})
}

// FilterResults drops entries below minCRAP, then truncates to the top N
// (0 means no truncation). Call SortResults first.
func FilterResults(results []Result, minCRAP float64, top int) []Result {
	filtered := results[:0:0]
	for _, r := range results {
		if r.CRAP >= minCRAP {
			filtered = append(filtered, r)
		}
	}
	if top > 0 && len(filtered) > top {
		filtered = filtered[:top]
	}
	return filtered
}

// FuncReport is the JSON-serializable shape of one Result.
type FuncReport struct {
	File          string  `json:"file"`
	Function      string  `json:"function"`
	StartLine     int     `json:"start_line"`
	EndLine       int     `json:"end_line"`
	Complexity    int     `json:"complexity"`
	Coverage      float64 `json:"coverage"`
	CoverageKnown bool    `json:"coverage_known"`
	CRAP          float64 `json:"crap"`
}

// ToReports converts Results to their JSON-serializable form.
func ToReports(results []Result) []FuncReport {
	out := make([]FuncReport, len(results))
	for i, r := range results {
		out[i] = FuncReport{
			File:          r.File,
			Function:      r.Name,
			StartLine:     r.StartLine,
			EndLine:       r.EndLine,
			Complexity:    r.Complexity,
			Coverage:      r.Coverage,
			CoverageKnown: r.CoverageKnown,
			CRAP:          r.CRAP,
		}
	}
	return out
}

// WriteTable renders results as an aligned text table.
func WriteTable(w io.Writer, results []Result) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CRAP\tCOMPLEXITY\tCOVERAGE\tFUNCTION\tLOCATION")

	anyUnmeasured := false
	for _, r := range results {
		cov := fmt.Sprintf("%.1f%%", r.Coverage*100)
		if !r.CoverageKnown {
			cov += "*"
			anyUnmeasured = true
		}
		fmt.Fprintf(tw, "%.1f\t%d\t%s\t%s\t%s:%d\n", r.CRAP, r.Complexity, cov, r.Name, r.File, r.StartLine)
	}
	tw.Flush()

	fmt.Fprintf(w, "\n%d function(s) analyzed.\n", len(results))
	if anyUnmeasured {
		fmt.Fprintln(w, "* no coverage data found for this function; assumed 0% (worst case)")
	}
}
