package main

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestComputeCRAP(t *testing.T) {
	cases := []struct {
		name       string
		complexity int
		coverage   float64
		want       float64
	}{
		{"fully covered simple", 1, 1.0, 1},
		{"uncovered simple", 1, 0.0, 2},
		{"fully covered complex", 10, 1.0, 10},
		{"uncovered complex", 10, 0.0, 110}, // 100*1 + 10
		{"half covered", 4, 0.5, 4*4*0.125 + 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeCRAP(c.complexity, c.coverage)
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("ComputeCRAP(%d, %v) = %v, want %v", c.complexity, c.coverage, got, c.want)
			}
		})
	}
}

func TestBuildResults_UnmeasuredDefaultsToZero(t *testing.T) {
	funcs := []FunctionInfo{
		{File: "a.go", Name: "Foo", StartLine: 1, EndLine: 5, Complexity: 5},
	}
	idx := &CoverageIndex{}
	results := BuildResults(funcs, idx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.CoverageKnown {
		t.Errorf("expected CoverageKnown = false")
	}
	if r.Coverage != 0 {
		t.Errorf("expected Coverage = 0, got %v", r.Coverage)
	}
	if want := ComputeCRAP(5, 0); r.CRAP != want {
		t.Errorf("CRAP = %v, want %v", r.CRAP, want)
	}
}

func TestSortAndFilterResults(t *testing.T) {
	results := []Result{
		{FunctionInfo: FunctionInfo{Name: "low"}, CRAP: 5},
		{FunctionInfo: FunctionInfo{Name: "high"}, CRAP: 50},
		{FunctionInfo: FunctionInfo{Name: "mid"}, CRAP: 20},
	}
	SortResults(results)
	if results[0].Name != "high" || results[1].Name != "mid" || results[2].Name != "low" {
		t.Fatalf("unexpected order: %+v", results)
	}

	filtered := FilterResults(results, 10, 0)
	if len(filtered) != 2 {
		t.Fatalf("min-crap filter: got %d results, want 2: %+v", len(filtered), filtered)
	}

	top1 := FilterResults(results, 0, 1)
	if len(top1) != 1 || top1[0].Name != "high" {
		t.Fatalf("top-1 filter: got %+v", top1)
	}
}

func TestWriteTable(t *testing.T) {
	results := []Result{
		{
			FunctionInfo:  FunctionInfo{File: "a.go", Name: "Risky", StartLine: 10, Complexity: 12},
			Coverage:      0,
			CoverageKnown: false,
			CRAP:          144,
		},
		{
			FunctionInfo:  FunctionInfo{File: "a.go", Name: "Safe", StartLine: 20, Complexity: 2},
			Coverage:      1.0,
			CoverageKnown: true,
			CRAP:          2,
		},
	}
	var buf bytes.Buffer
	WriteTable(&buf, results)
	out := buf.String()

	if !strings.Contains(out, "Risky") || !strings.Contains(out, "Safe") {
		t.Errorf("table missing function names: %s", out)
	}
	if !strings.Contains(out, "0.0%*") {
		t.Errorf("expected unmeasured marker '0.0%%*': %s", out)
	}
	if !strings.Contains(out, "100.0%") {
		t.Errorf("expected measured coverage '100.0%%': %s", out)
	}
	if !strings.Contains(out, "2 function(s) analyzed") {
		t.Errorf("expected summary line: %s", out)
	}
}
