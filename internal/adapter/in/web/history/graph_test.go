package history

import (
	"fmt"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func c(hash string, parents ...string) domain.Commit {
	return domain.Commit{Hash: hash, Parents: parents}
}

// rows renders a layout as text, one row each: the dot's lane, then each
// stroke as lane+height → lane+height (t top, m the dot, b bottom).
func rows(gs []Graph) []string {
	col := func(px int) int { return (px - LaneWidth/2) / LaneWidth }
	at := func(y int) string { return map[int]string{0: "t", RowHeight / 2: "m", RowHeight: "b"}[y] }
	var out []string
	for _, g := range gs {
		var strokes []string
		for _, l := range g.Lines {
			strokes = append(strokes, fmt.Sprintf("%d%s→%d%s", col(l.X1), at(l.Y1), col(l.X2), at(l.Y2)))
		}
		out = append(out, fmt.Sprintf("%d | %s", col(g.Dot.X), strings.Join(strokes, " ")))
	}
	return out
}

func assertRows(t *testing.T, gs []Graph, want ...string) {
	t.Helper()
	if got := rows(gs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("layout:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLayoutALinearHistoryStaysInOneLane(t *testing.T) {
	assertRows(t, Layout([]domain.Commit{c("c3", "c2"), c("c2", "c1"), c("c1")}),
		"0 | 0m→0b",
		"0 | 0t→0m 0m→0b",
		"0 | 0t→0m",
	)
}

// A branch merged back:
//
//	M      merge
//	|\
//	| F    feature
//	A |    main work
//	|/
//	B      base
func TestLayoutAMergeOpensALaneThatJoinsBack(t *testing.T) {
	gs := Layout([]domain.Commit{c("M", "A", "F"), c("F", "B"), c("A", "B"), c("B")})
	assertRows(t, gs,
		"0 | 0m→0b 0m→1b", // first parent below, the second opens lane 1
		"1 | 0t→0b 1t→1m 1m→1b",
		"0 | 0t→0m 1t→0b 0m→0b", // B is expected in lane 1 too: that lane joins A's, the mainline stays left
		"0 | 0t→0m",
	)
	if !gs[0].Dot.Merge || gs[1].Dot.Merge {
		t.Error("only the merge is drawn hollow")
	}
}

func TestLayoutBranchTipsTakeFreeLanes(t *testing.T) {
	assertRows(t, Layout([]domain.Commit{c("X", "B"), c("Y", "B"), c("B")}),
		"0 | 0m→0b",       // a tip: nothing from above
		"1 | 0t→0b 1m→0b", // the second tip's parent is already expected: it joins lane 0
		"0 | 0t→0m",
	)
}

func TestLayoutAParentBeyondTheLimitKeepsItsLaneGoing(t *testing.T) {
	gs := Layout([]domain.Commit{c("Z", "beyond")})
	assertRows(t, gs, "0 | 0m→0b")
	if gs[0].Lines[0].D() != fmt.Sprintf("M%d %dV%d", LaneWidth/2, RowHeight/2, RowHeight) {
		t.Errorf("path = %s", gs[0].Lines[0].D())
	}
}

func TestLineDCurvesBetweenLanes(t *testing.T) {
	if d := (Line{X1: 7, Y1: 0, X2: 21, Y2: 32}).D(); d != "M7 0C7 16 21 16 21 32" {
		t.Errorf("d = %s", d)
	}
}
