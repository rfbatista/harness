package bench

import (
	"encoding/json"
	"fmt"
	"reflect"

	"operators-mcp/internal/domain"
)

// ScoreToolUse scores a trace of calls against the case's expected calls.
//
// Each expected call is matched to the first unused call of the same tool, in
// expectation order. Right tool is matched / max(expected, made): a missing
// and an extra call both cost. Valid parameters is the share of matched calls
// whose arguments fit (a missing call is not a parameter fault). Right order,
// only when the case expects one, is the share of matched calls in the longest
// stretch that keeps the expected order.
func ScoreToolUse(c domain.BenchCase, trace []domain.BenchCall) domain.BenchToolUseScores {
	exp := c.Expect.Tools
	if len(exp) == 0 {
		return domain.BenchToolUseScores{}
	}
	used := make([]bool, len(trace))
	match := make([]int, len(exp)) // trace index per expected call, -1 when missing
	var rt domain.BenchPatternScore
	rt.Scored = true
	matched := 0
	for i, e := range exp {
		match[i] = -1
		for j, got := range trace {
			if !used[j] && got.Tool == e.Tool {
				used[j], match[i] = true, j
				matched++
				break
			}
		}
		if match[i] < 0 {
			rt.Reasons = append(rt.Reasons, fmt.Sprintf("expected call %d to %s is missing", i+1, e.Tool))
		}
	}
	for j, got := range trace {
		if !used[j] {
			rt.Reasons = append(rt.Reasons, fmt.Sprintf("unexpected call %d to %s", j+1, got.Tool))
		}
	}
	rt.Score = ratio(matched, max(len(exp), len(trace)))

	vp := domain.BenchPatternScore{Scored: true}
	valid := 0
	for i, e := range exp {
		if match[i] < 0 {
			continue
		}
		if paramsFit(e.Params, trace[match[i]].Params) {
			valid++
		} else {
			vp.Reasons = append(vp.Reasons, fmt.Sprintf("call %d to %s has the wrong parameters", match[i]+1, e.Tool))
		}
	}
	vp.Score = ratio(valid, matched)

	var ro domain.BenchPatternScore
	if c.Expect.Ordered {
		ro.Scored = true
		var seq []int
		for i := range exp {
			if match[i] >= 0 {
				seq = append(seq, match[i])
			}
		}
		keep := longestIncreasing(seq)
		ro.Score = ratio(keep, matched)
		if keep < matched {
			for i, e := range exp {
				if match[i] >= 0 && i > 0 && outOfOrder(match, i) {
					ro.Reasons = append(ro.Reasons, fmt.Sprintf("call to %s came before the call that should precede it", e.Tool))
				}
			}
		}
	}
	return domain.BenchToolUseScores{RightTool: rt, ValidParams: vp, RightOrder: ro}
}

// outOfOrder reports whether the matched expected call i was made before an
// earlier matched expected call.
func outOfOrder(match []int, i int) bool {
	for k := 0; k < i; k++ {
		if match[k] >= 0 && match[k] > match[i] {
			return true
		}
	}
	return false
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// longestIncreasing is the length of the longest strictly increasing
// subsequence.
func longestIncreasing(seq []int) int {
	best := make([]int, len(seq))
	longest := 0
	for i := range seq {
		best[i] = 1
		for k := 0; k < i; k++ {
			if seq[k] < seq[i] && best[k]+1 > best[i] {
				best[i] = best[k] + 1
			}
		}
		longest = max(longest, best[i])
	}
	return longest
}

// paramsFit reports whether the call's arguments carry every expected
// parameter with an equal value; nil expectations accept anything.
func paramsFit(want, got map[string]any) bool {
	for k, w := range want {
		g, ok := got[k]
		if !ok || !sameJSON(w, g) {
			return false
		}
	}
	return true
}

// sameJSON compares two values as JSON, so 1 and 1.0 and a decoded number agree.
func sameJSON(a, b any) bool {
	var x, y any
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if json.Unmarshal(ab, &x) != nil || json.Unmarshal(bb, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
