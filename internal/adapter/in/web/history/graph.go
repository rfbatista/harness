// Package history serves a repository's history page: its commit graph,
// drawn row by row next to each commit, and one commit's details.
package history

import (
	"fmt"
	"slices"

	"operators-mcp/internal/domain"
)

// The graph's geometry, in pixels: a lane's width and a row's height. The
// history CSS block sizes rows to RowHeight so the rows' drawings join.
const (
	LaneWidth = 14
	RowHeight = 32
	dotRadius = 4
	lanes     = 8 // colours the CSS cycles through
)

// Graph is one row's slice of the commit graph.
type Graph struct {
	Width int
	Lines []Line
	Dot   Dot
}

// Line is one stroke of a row, from (X1, Y1) to (X2, Y2), in its lane's
// colour.
type Line struct {
	X1, Y1, X2, Y2 int
	Lane           int
}

// D is the line as an SVG path: straight down a lane, or an S-curve
// between two.
func (l Line) D() string {
	if l.X1 == l.X2 {
		return fmt.Sprintf("M%d %dV%d", l.X1, l.Y1, l.Y2)
	}
	mid := (l.Y1 + l.Y2) / 2
	return fmt.Sprintf("M%d %dC%d %d %d %d %d %d", l.X1, l.Y1, l.X1, mid, l.X2, mid, l.X2, l.Y2)
}

// Dot is the row's commit; Merge draws it hollow.
type Dot struct {
	X, Y, R int
	Lane    int
	Merge   bool
}

// Layout lays commits — children before parents, as git log --topo-order
// lists them — out in lanes, and returns each row's drawing.
//
// Lanes hold the commit each expects next. A commit takes the lane that
// expects it (the leftmost, when several children do: they merge into it
// there) or, as a branch tip, the first free lane. Its first parent then
// continues in its lane; other parents open lanes of their own, unless a
// lane already expects them (a first parent expected further right moves
// into this lane: the mainline stays left).
func Layout(commits []domain.Commit) []Graph {
	var current []string
	out := make([]Graph, 0, len(commits))
	for _, c := range commits {
		col := slices.Index(current, c.Hash)
		tip := col < 0
		if tip {
			col = freeLane(&current)
			current[col] = c.Hash
		}
		before := slices.Clone(current)

		after := slices.Clone(current)
		for i := range after {
			if after[i] == c.Hash {
				after[i] = ""
			}
		}
		for k, p := range c.Parents {
			if j := slices.Index(after, p); j >= 0 {
				// Already expected. The first parent is pulled into this
				// lane when that lane is further right, so the line a
				// commit's first parents make stays as far left as it can.
				if k == 0 && j > col {
					after[j], after[col] = "", p
				}
				continue
			}
			if k == 0 {
				after[col] = p
			} else {
				after[freeLane(&after)] = p
			}
		}
		for len(after) > 0 && after[len(after)-1] == "" {
			after = after[:len(after)-1]
		}

		g := Graph{
			Width: max(len(before), len(after), 1) * LaneWidth,
			Dot:   Dot{X: x(col), Y: RowHeight / 2, R: dotRadius, Lane: col % lanes, Merge: len(c.Parents) > 1},
		}
		for i, s := range before {
			switch {
			case s == "":
			case s == c.Hash:
				if i == col && tip {
					continue // a branch tip: nothing comes from above
				}
				g.Lines = append(g.Lines, Line{X1: x(i), Y1: 0, X2: x(col), Y2: RowHeight / 2, Lane: i % lanes})
			default:
				j := slices.Index(after, s)
				g.Lines = append(g.Lines, Line{X1: x(i), Y1: 0, X2: x(j), Y2: RowHeight, Lane: j % lanes})
			}
		}
		for _, p := range c.Parents {
			j := slices.Index(after, p)
			g.Lines = append(g.Lines, Line{X1: x(col), Y1: RowHeight / 2, X2: x(j), Y2: RowHeight, Lane: j % lanes})
		}
		out = append(out, g)
		current = after
	}
	return out
}

// freeLane returns the first lane nobody expects, adding one if none is free.
func freeLane(lanes *[]string) int {
	if i := slices.Index(*lanes, ""); i >= 0 {
		return i
	}
	*lanes = append(*lanes, "")
	return len(*lanes) - 1
}

func x(col int) int { return col*LaneWidth + LaneWidth/2 }
