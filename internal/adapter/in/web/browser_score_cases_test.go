package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// casesAPI is a project's bench cases that can be edited, added and deleted, with one scored run and one
// recorded trajectory, in the shapes of the bench API.
type casesAPI struct {
	mu      sync.Mutex
	cases   []map[string]any
	patched []string
	deleted []string
	runs    []string
}

func (a *casesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/bench/cases"):
		json.NewEncoder(w).Encode(map[string]any{"cases": a.cases})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/bench/scorecards"):
		card := benchCard("s1", "2026-11-26T11:40:00Z", "fp1", 1.14, [4]any{"a", 0.5, 0.5, 0.5})
		card["cases"].([]map[string]any)[0]["calls"] = []any{map[string]any{"tool": "write_file", "params": map[string]any{"path": "x"}}, map[string]any{"tool": "read_file", "params": map[string]any{"path": "README.md"}}}
		card["cases"].([]map[string]any)[0]["steps"] = []any{map[string]any{"step": 1, "ok": false, "note": "early: read first"}, map[string]any{"step": 2, "ok": true, "note": "expected"}}
		card["cases"].([]map[string]any)[0]["trajectory"] = map[string]any{"scored": true, "score": 0.5, "reasons": []any{"It wrote before it read."}}
		json.NewEncoder(w).Encode(map[string]any{"scorecards": []any{card}})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/compare"):
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": "there is no earlier scorecard to compare with", "code": "BENCH_NO_PREVIOUS_SCORECARD"})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/bench/runs"):
		body, _ := io.ReadAll(r.Body)
		a.runs = append(a.runs, string(body))
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{"id": "run9", "project_id": "p1", "status": "running", "total": 1, "done": 0, "started_at": "2026-11-26T12:00:00Z"})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/bench/trajectories"):
		json.NewEncoder(w).Encode(map[string]any{"trajectories": []any{map[string]any{"id": "t1", "task_id": "task1", "prompt": "Fix the failing test", "steps": []any{map[string]any{}, map[string]any{}}, "recorded_at": "2026-11-26T09:00:00Z"}}})
	case r.Method == http.MethodPatch && strings.Contains(path, "/bench/cases/"):
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Expect map[string]any `json:"expect"`
		}
		_ = json.Unmarshal(body, &in)
		tools, _ := in.Expect["tools"].([]any)
		for _, t := range tools {
			if tool, _ := t.(map[string]any)["tool"].(string); tool != "read_file" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"field": "expect.tools[0].tool", "code": "UNKNOWN_TOOL", "message": "tool " + tool + " has no mock"}}})
				return
			}
		}
		a.patched = append(a.patched, string(body))
		a.cases[0]["expect"] = in.Expect
		json.NewEncoder(w).Encode(a.cases[0])
	case r.Method == http.MethodDelete && strings.Contains(path, "/bench/cases/"):
		a.deleted = append(a.deleted, path[strings.LastIndex(path, "/")+1:])
		a.cases = a.cases[1:]
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/bench/cases/from-trajectory"):
		a.cases = append(a.cases, map[string]any{"id": "c2", "name": "", "prompt": "Fix the failing test", "tools": []any{}, "expect": map[string]any{}, "source": "trajectory"})
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(a.cases[1])
	default:
		http.NotFound(w, r)
	}
}

// TestScoreCasesViewInTheBrowser: a case opens in place to its trace with the judge's word per step; Edit
// expectation saves through the API and a refusal lands on the form; From a recorded trajectory makes a case;
// Delete asks once.
func TestScoreCasesViewInTheBrowser(t *testing.T) {
	api := &casesAPI{cases: []map[string]any{{"id": "a", "name": "a", "prompt": "Read the README.", "tools": []any{map[string]any{"name": "read_file", "result": "# Demo"}}, "expect": map[string]any{"tools": []any{map[string]any{"tool": "read_file"}}, "ordered": true}, "source": "definition"}}}
	srv := scoreServer(t, api)
	ctx, errs := browser(t)

	click := func(sel string) chromedp.Action {
		return chromedp.Evaluate(`document.querySelector(`+jsString(sel)+`).click()`, nil)
	}
	wait := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	var trace, refused, expected, trajectories, afterDelete string
	steps := []chromedp.Action{
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/score"),
		wait(`!!document.querySelector('.score-tile')`),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label=View] button')].find(b => b.textContent.startsWith('Cases')).click()`, nil),
		wait(`!!document.querySelector('.sc-row[data-kind=case]')`),
		click(`.sc-row[data-kind=case]`),
		wait(`!!document.querySelector('.trace')`),
		chromedp.Evaluate(`document.querySelector('[data-detail]').textContent`, &trace),
		click(`[data-action=replay]`),
		wait(`document.querySelector('[data-action=replay]').disabled`),
		click(`[data-action=edit-expectation]`),
		wait(`!!document.querySelector('form[data-form=edit]')`),
		chromedp.Evaluate(`(() => { const t = document.querySelector('textarea[name=calls]'); const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set; set.call(t, 'write_file'); t.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		chromedp.Evaluate(`document.querySelector('form[data-form=edit]').requestSubmit()`, nil),
		wait(`!!document.querySelector('form[data-form=edit] .error')`),
		chromedp.Evaluate(`document.querySelector('form[data-form=edit] .error').textContent`, &refused),
		chromedp.Evaluate(`(() => { const t = document.querySelector('textarea[name=calls]'); const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set; set.call(t, 'read_file {"path": "README.md"}'); t.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		chromedp.Evaluate(`document.querySelector('form[data-form=edit]').requestSubmit()`, nil),
		wait(`!document.querySelector('form[data-form=edit]')`),
		wait(`!!document.querySelector('[data-expectation]')`),
		chromedp.Evaluate(`document.querySelector('[data-expectation]').textContent`, &expected),
		click(`[data-action=from-trajectory]`),
		wait(`!!document.querySelector('[data-trajectory]')`),
		chromedp.Evaluate(`document.querySelector('[data-trajectories]').textContent`, &trajectories),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-trajectory] button')][0].click()`, nil),
		wait(`document.querySelectorAll('.sc-row[data-kind=case]').length === 2`),
		wait(`!!document.querySelector('[data-action=delete]')`),
		click(`[data-action=delete]`),
		wait(`!!document.querySelector('[data-action=confirm-delete]')`),
		click(`[data-action=confirm-delete]`),
		wait(`document.querySelectorAll('.sc-row[data-kind=case]').length === 1`),
		chromedp.Evaluate(`document.querySelector('.score').textContent`, &afterDelete),
	}
	for i, step := range steps {
		if err := chromedp.Run(ctx, step); err != nil {
			t.Fatalf("step %d: %v\nJS errors: %v", i, err, errs.all())
		}
	}
	for _, want := range []string{"Read the README.", "write_file", "early: read first", "expected", "It wrote before it read."} {
		if !strings.Contains(trace, want) {
			t.Errorf("the trace %q lacks %q", trace, want)
		}
	}
	if len(api.runs) != 1 || !strings.Contains(api.runs[0], `"case_ids":["a"]`) {
		t.Errorf("Replay this case started %v, want one run of case a", api.runs)
	}
	if !strings.Contains(refused, "UNKNOWN_TOOL") || !strings.Contains(refused, "write_file has no mock") {
		t.Errorf("the refusal reads %q", refused)
	}
	if len(api.patched) != 1 || !strings.Contains(api.patched[0], `"path":"README.md"`) {
		t.Errorf("patched %v", api.patched)
	}
	if !strings.Contains(expected, "read_file") {
		t.Errorf("the expectation reads %q", expected)
	}
	if !strings.Contains(trajectories, "Fix the failing test") || !strings.Contains(trajectories, "2 steps") {
		t.Errorf("trajectories %q", trajectories)
	}
	if len(api.deleted) != 1 {
		t.Errorf("deleted %v, want one case after one confirmation", api.deleted)
	}
	_ = afterDelete
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
