package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// startRunBody starts a run with a JSON body and waits for it to end.
func startRunBody(t *testing.T, h *Handler, body string) (int, map[string]any) {
	t.Helper()
	code, run := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/runs", body)
	if code == http.StatusAccepted {
		return code, waitRunEnds(t, h, run["id"].(string))
	}
	return code, run
}

func twoCases(t *testing.T, h *Handler) (first, second string) {
	t.Helper()
	for i, name := range []string{"first", "second"} {
		body := strings.Replace(benchCaseBody, "reads the readme", name, 1)
		rec := postJSON(t, h, "/projects/p1/bench/cases", json.RawMessage(body))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create case %d = %d: %s", i, rec.Code, rec.Body.String())
		}
		var c map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &c)
		if i == 0 {
			first = c["id"].(string)
		} else {
			second = c["id"].(string)
		}
	}
	return first, second
}

func TestACompareWithABaselineSetsTheScorecardAgainstThatRun(t *testing.T) {
	h, release := benchRunsHandler(t)
	twoCases(t, h)
	close(release)
	var ids []string
	for range 3 {
		_, run := startRunBody(t, h, "")
		ids = append(ids, run["id"].(string))
	}

	_, byDefault := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+ids[2]+"/compare")
	if byDefault["previous_id"] != ids[1] {
		t.Fatalf("default baseline = %v, want the run just before (%s)", byDefault["previous_id"], ids[1])
	}
	code, chosen := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+ids[2]+"/compare?baseline="+ids[0])
	if code != http.StatusOK || chosen["previous_id"] != ids[0] {
		t.Fatalf("chosen baseline = %d %v", code, chosen)
	}
	// The three columns of the scorecard have a delta of their own.
	groups, _ := chosen["groups"].(map[string]any)
	for _, g := range []string{"tool_use", "outcome", "trajectory"} {
		if _, ok := groups[g]; !ok {
			t.Fatalf("groups = %v, want %s", groups, g)
		}
	}
}

func TestACompareBaselineThatDoesNotExistIs404AndAnotherProjectsIs422(t *testing.T) {
	h, release, cards := benchRunsHandlerWithStore(t)
	twoCases(t, h)
	close(release)
	startRunBody(t, h, "")
	_, cur := startRunBody(t, h, "")
	id := cur["id"].(string)

	if code, body := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+id+"/compare?baseline=nope"); code != http.StatusNotFound || body["code"] != "BENCH_SCORECARD_NOT_FOUND" {
		t.Fatalf("unknown baseline = %d %v", code, body)
	}
	other := domain.Scorecard{ID: "foreign", ProjectID: "p2", Status: domain.ScorecardFinished, StartedAt: time.Now().Add(-time.Hour)}
	if err := cards.Save(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if code, body := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+id+"/compare?baseline=foreign"); code != http.StatusUnprocessableEntity || body["code"] != "BENCH_BASELINE_OTHER_PROJECT" {
		t.Fatalf("another project's baseline = %d %v", code, body)
	}
}

func TestARunOfSomeCasesIsPartialAndNeverTheDefaultBaseline(t *testing.T) {
	h, release := benchRunsHandler(t)
	first, _ := twoCases(t, h)
	close(release)
	_, full := startRunBody(t, h, "")
	code, some := startRunBody(t, h, `{"case_ids": ["`+first+`"]}`)
	if code != http.StatusAccepted || some["status"] != domain.BenchRunFinished {
		t.Fatalf("limited run = %d %v", code, some)
	}
	if some["total"] != float64(1) {
		t.Fatalf("a run of one case has total %v", some["total"])
	}
	_, card := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+some["id"].(string))
	if card["status"] != domain.ScorecardPartial || len(card["cases"].([]any)) != 1 {
		t.Fatalf("scorecard of a limited run = %v", card)
	}

	_, third := startRunBody(t, h, "")
	_, cmp := benchRunBody(t, h, http.MethodGet, "/projects/p1/bench/scorecards/"+third["id"].(string)+"/compare")
	if cmp["previous_id"] != full["id"] {
		t.Fatalf("the default baseline is %v, want the full run %v: a partial run is never one", cmp["previous_id"], full["id"])
	}
}

func TestARunOfCaseIdsThatAreNotTheProjectsIs422NamingThem(t *testing.T) {
	h, release := benchRunsHandler(t)
	twoCases(t, h)
	close(release)
	// A case of another project.
	rec := postJSON(t, h, "/projects/p2/bench/cases", json.RawMessage(benchCaseBody))
	var foreign map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &foreign)

	for name, body := range map[string]string{"unknown": `{"case_ids": ["ghost"]}`, "another project's": `{"case_ids": ["` + foreign["id"].(string) + `"]}`} {
		code, out := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/runs", body)
		if code != http.StatusUnprocessableEntity || out["code"] != "BENCH_CASE_IDS_INVALID" {
			t.Fatalf("%s: %d %v", name, code, out)
		}
		if msg, _ := out["error"].(string); !strings.Contains(msg, "ghost") && !strings.Contains(msg, foreign["id"].(string)) {
			t.Fatalf("%s: the message must name the ids: %q", name, msg)
		}
	}
}
