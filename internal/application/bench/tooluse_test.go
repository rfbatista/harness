package bench

import (
	"reflect"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func call(tool string, kv ...any) domain.BenchCall {
	c := domain.BenchCall{Tool: tool}
	if len(kv) > 0 {
		c.Params = map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			c.Params[kv[i].(string)] = kv[i+1]
		}
	}
	return c
}

func expected(ordered bool, calls ...domain.BenchExpectedCall) domain.BenchCase {
	return domain.BenchCase{Expect: domain.BenchExpectation{Tools: calls, Ordered: ordered}}
}

var (
	readReadme = domain.BenchExpectedCall{Tool: "read_file", Params: map[string]any{"path": "README.md"}}
	editMain   = domain.BenchExpectedCall{Tool: "edit_file", Params: map[string]any{"path": "main.go"}}
)

func scores(r domain.BenchToolUseScores) [3]float64 {
	return [3]float64{r.RightTool.Score, r.ValidParams.Score, r.RightOrder.Score}
}

func TestToolUseExactMatchScoresOneOnAllThreePatterns(t *testing.T) {
	c := expected(true, readReadme, editMain)
	got := ScoreToolUse(c, []domain.BenchCall{call("read_file", "path", "README.md"), call("edit_file", "path", "main.go")})
	if scores(got) != [3]float64{1, 1, 1} {
		t.Fatalf("scores = %v", scores(got))
	}
}

func TestToolUseEachFaultLowersOnlyItsOwnPattern(t *testing.T) {
	c := expected(true, readReadme, editMain)
	cases := []struct {
		name   string
		trace  []domain.BenchCall
		scores [3]float64
		reason string
	}{
		{"missing tool", []domain.BenchCall{call("read_file", "path", "README.md")}, [3]float64{0.5, 1, 1}, "edit_file"},
		{"extra tool", []domain.BenchCall{call("read_file", "path", "README.md"), call("edit_file", "path", "main.go"), call("delete_file", "path", "x")}, [3]float64{2.0 / 3, 1, 1}, "delete_file"},
		{"wrong parameter", []domain.BenchCall{call("read_file", "path", "README.md"), call("edit_file", "path", "other.go")}, [3]float64{1, 0.5, 1}, "edit_file"},
		{"swapped order", []domain.BenchCall{call("edit_file", "path", "main.go"), call("read_file", "path", "README.md")}, [3]float64{1, 1, 0.5}, "edit_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScoreToolUse(c, tc.trace)
			if scores(got) != tc.scores {
				t.Fatalf("scores = %v, want %v", scores(got), tc.scores)
			}
			var reasons []string
			for _, p := range []domain.BenchPatternScore{got.RightTool, got.ValidParams, got.RightOrder} {
				reasons = append(reasons, p.Reasons...)
			}
			if !strings.Contains(strings.Join(reasons, "\n"), tc.reason) {
				t.Fatalf("no reason names %s: %v", tc.reason, reasons)
			}
		})
	}
}

func TestToolUseEmptyTraceAgainstExpectedToolsScoresZero(t *testing.T) {
	got := ScoreToolUse(expected(true, readReadme), nil)
	if scores(got) != [3]float64{0, 0, 0} || !got.RightTool.Scored || !got.ValidParams.Scored || !got.RightOrder.Scored {
		t.Fatalf("got = %+v", got)
	}
}

func TestToolUseWithoutExpectationLeavesPatternsUnscored(t *testing.T) {
	c := domain.BenchCase{Expect: domain.BenchExpectation{Reference: "done"}}
	got := ScoreToolUse(c, []domain.BenchCall{call("read_file")})
	if got.RightTool.Scored || got.ValidParams.Scored || got.RightOrder.Scored {
		t.Fatalf("got = %+v", got)
	}
}

func TestToolUseOrderIsUnscoredWhenTheCaseDoesNotExpectOne(t *testing.T) {
	got := ScoreToolUse(expected(false, readReadme, editMain), []domain.BenchCall{call("edit_file", "path", "main.go"), call("read_file", "path", "README.md")})
	if got.RightOrder.Scored || got.RightTool.Score != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestToolUseExpectedCallWithoutParamsAcceptsAnyArguments(t *testing.T) {
	c := expected(true, domain.BenchExpectedCall{Tool: "read_file"})
	got := ScoreToolUse(c, []domain.BenchCall{call("read_file", "path", "anything")})
	if scores(got) != [3]float64{1, 1, 1} {
		t.Fatalf("scores = %v", scores(got))
	}
}

func TestToolUseScoringIsDeterministic(t *testing.T) {
	c := expected(true, readReadme, editMain)
	trace := []domain.BenchCall{call("edit_file", "path", "x"), call("read_file", "path", "README.md"), call("zzz")}
	if a, b := ScoreToolUse(c, trace), ScoreToolUse(c, trace); !reflect.DeepEqual(a, b) {
		t.Fatalf("%+v != %+v", a, b)
	}
}
