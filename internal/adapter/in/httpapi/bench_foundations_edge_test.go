package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// A PATCH with a body that is not JSON, or no object, changes nothing and answers 4xx.
func TestBenchCasePatchWithAMalformedBodyChangesNothing(t *testing.T) {
	h := benchHandler(t, ":memory:")
	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	var created domain.BenchCase
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	path := "/projects/p1/bench/cases/" + created.ID
	for name, body := range map[string]string{"empty": ``, "not json": `{"expect":`, "array": `[]`, "null": `null`} {
		if rec := benchPatch(t, h, path, body); rec.Code < 400 || rec.Code >= 500 {
			t.Errorf("%s: status %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	var list struct {
		Cases []domain.BenchCase `json:"cases"`
	}
	_ = json.Unmarshal(getJSON(t, h, "/projects/p1/bench/cases").Body.Bytes(), &list)
	if len(list.Cases) != 1 || list.Cases[0].Expect.Reference != "A demo project." {
		t.Fatalf("a refused patch changed the case: %+v", list.Cases)
	}
}

// Editing the expectation twice with the same body is the same case, and the prompt cannot be edited through it.
func TestBenchCasePatchIsIdempotentAndLeavesThePromptAlone(t *testing.T) {
	h := benchHandler(t, ":memory:")
	rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(benchCaseBody))
	var created domain.BenchCase
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	path := "/projects/p1/bench/cases/" + created.ID
	body := `{"prompt": "something else", "expect": {"tools": [{"tool": "read_file"}], "reference": "R"}}`
	var first, second string
	for i, dst := range []*string{&first, &second} {
		rec := benchPatch(t, h, path, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("patch %d = %d: %s", i, rec.Code, rec.Body.String())
		}
		*dst = rec.Body.String()
	}
	if first != second {
		t.Errorf("same patch twice answered differently:\n%s\n%s", first, second)
	}
	var c domain.BenchCase
	_ = json.Unmarshal([]byte(second), &c)
	if c.Prompt != created.Prompt {
		t.Errorf("the prompt changed through the expectation edit: %q", c.Prompt)
	}
}

// case_ids: an empty list is a run of nothing and must not silently become a run of every case.
func TestARunWithAnEmptyCaseIdsListIsNotARunOfEveryCase(t *testing.T) {
	h, release := benchRunsHandler(t)
	twoCases(t, h)
	close(release)
	code, run := startRunBody(t, h, `{"case_ids": []}`)
	if code == http.StatusAccepted && run["total"] == float64(2) {
		t.Fatalf("case_ids [] ran all cases (total %v); want a refusal or an empty run", run["total"])
	}
}

// case_ids naming the same case twice runs it once.
func TestARunWithADuplicatedCaseIDRunsItOnce(t *testing.T) {
	h, release := benchRunsHandler(t)
	first, _ := twoCases(t, h)
	close(release)
	code, run := startRunBody(t, h, `{"case_ids": ["`+first+`","`+first+`"]}`)
	if code != http.StatusAccepted || run["total"] != float64(1) {
		t.Fatalf("duplicate id run = %d total %v", code, run["total"])
	}
}

// A run compared with itself as the baseline answers the run against itself with no regression.
func TestACompareWithItselfAsTheBaselineHasNoDeltas(t *testing.T) {
	h, release := benchRunsHandler(t)
	twoCases(t, h)
	close(release)
	_, run := startRunBody(t, h, "")
	id := run["id"].(string)
	code, cmp := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+id+"/compare?baseline="+id)
	if code != http.StatusOK {
		t.Fatalf("compare with itself = %d %v", code, cmp)
	}
	if s := toJSONString(cmp); strings.Contains(s, `"regressed":[`) && !strings.Contains(s, `"regressed":[]`) && !strings.Contains(s, `"regressed":null`) {
		t.Errorf("a run regressed against itself: %s", s)
	}
}

func toJSONString(v any) string { b, _ := json.Marshal(v); return string(b) }
