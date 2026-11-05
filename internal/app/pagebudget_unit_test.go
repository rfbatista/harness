//go:build !windows

package app

import (
	"strings"
	"testing"
	"time"
)

// The budget check names each page that is over its budget, with its median
// and its budget, and each page the file does not cover.
func TestThePageBudgetCheckNamesEachPageOverItsBudget(t *testing.T) {
	b := pageBudgets{Frame: 100, Pages: map[string]int{"board": 300, "usage": 300}}
	rows := []benchRow{
		{page: "board", median: 250 * time.Millisecond, breakdown: true, frame: 90 * time.Millisecond},
		{page: "usage", median: 480 * time.Millisecond, breakdown: true, frame: 150 * time.Millisecond},
		{page: "reviews", median: time.Millisecond},
	}
	over := strings.Join(b.overBudget(rows), "\n")
	for _, want := range []string{"usage: median 480ms is over its budget of 300ms", "usage: frame share 150ms is over the frame budget of 100ms", "reviews: no budget"} {
		if !strings.Contains(over, want) {
			t.Errorf("missing %q in:\n%s", want, over)
		}
	}
	if strings.Contains(over, "board") {
		t.Errorf("the board is within its budget:\n%s", over)
	}
}

// A page fragment builds no frame: any frame share is over its budget, and a
// fragment has a budget of its own like any page.
func TestThePageBudgetCheckHoldsAFragmentToNoFrame(t *testing.T) {
	b := pageBudgets{Frame: 100, Pages: map[string]int{"board fragment": 300}}
	rows := []benchRow{
		{page: "board fragment", median: 20 * time.Millisecond, breakdown: true, frame: 5 * time.Millisecond},
		{page: "usage fragment", median: 20 * time.Millisecond, breakdown: true},
	}
	over := strings.Join(b.overBudget(rows), "\n")
	for _, want := range []string{"board fragment: frame share 5ms, a fragment builds no frame", "usage fragment: no budget"} {
		if !strings.Contains(over, want) {
			t.Errorf("missing %q in:\n%s", want, over)
		}
	}
}

// The budgets file in the repository covers every project page.
func TestThePageBudgetsFileCoversEveryProjectPage(t *testing.T) {
	b, err := readPageBudgets("testdata/page-budgets.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"board", "task page", "settings", "documents", "code", "design", "design system", "usage", "reviews"} {
		if b.Pages[page] <= 0 {
			t.Errorf("no budget for the %s", page)
		}
		if b.Pages[page+" fragment"] <= 0 {
			t.Errorf("no budget for the %s as a fragment", page)
		}
	}
	if b.Frame <= 0 {
		t.Error("no budget for the frame share")
	}
}
