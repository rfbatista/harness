//go:build !windows

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// pageBudgets is the page-time budget file: a budget in milliseconds for each
// project page and for the frame share, the median on the seeded project.
type pageBudgets struct {
	Frame int            `json:"frame"`
	Pages map[string]int `json:"pages"`
}

func readPageBudgets(path string) (pageBudgets, error) {
	var b pageBudgets
	data, err := os.ReadFile(path)
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

// overBudget lists, one line each, the pages whose median is over their
// budget and the frame shares over the frame's. A page with no budget in the
// file is over it too: every project page must carry one, and so must its
// fragment, which also builds no frame.
func (b pageBudgets) overBudget(rows []benchRow) []string {
	var over []string
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	for _, r := range rows {
		budget, ok := b.Pages[r.page]
		switch {
		case !ok:
			over = append(over, fmt.Sprintf("%s: no budget in the budgets file", r.page))
		case r.median > ms(budget):
			over = append(over, fmt.Sprintf("%s: median %v is over its budget of %v", r.page, r.median.Round(time.Millisecond), ms(budget)))
		}
		if strings.HasSuffix(r.page, " fragment") {
			// A fragment is built without the shared project frame (ADR 0064).
			if r.breakdown && r.frame > 0 {
				over = append(over, fmt.Sprintf("%s: frame share %v, a fragment builds no frame", r.page, r.frame.Round(time.Microsecond)))
			}
			continue
		}
		if r.breakdown && b.Frame > 0 && r.frame > ms(b.Frame) {
			over = append(over, fmt.Sprintf("%s: frame share %v is over the frame budget of %v", r.page, r.frame.Round(time.Millisecond), ms(b.Frame)))
		}
	}
	return over
}
