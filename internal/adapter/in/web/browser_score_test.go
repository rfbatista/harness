package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// benchAPI is one project's eval bench in memory: its cases, its scorecards (the API writes them newest first)
// and the runs started. It answers the shapes of the bench API.
type benchAPI struct {
	mu        sync.Mutex
	cases     []map[string]any
	cards     []map[string]any
	runsStart int
	cancels   int
	runError  string // what GET .../runs/run9 says failed it
}

func benchCard(id, at, fingerprint string, cost float64, cases ...[4]any) map[string]any {
	var scored []map[string]any
	var sum float64
	for _, c := range cases {
		name, tool, outcome, traj := c[0].(string), c[1].(float64), c[2].(float64), c[3].(float64)
		scored = append(scored, map[string]any{
			"case_id": name, "case_name": name,
			"tool_use": map[string]any{"right_tool": map[string]any{"scored": true, "score": tool}, "valid_params": map[string]any{"scored": true, "score": tool}, "right_order": map[string]any{"scored": true, "score": tool}},
			"outcome":  map[string]any{"scored": true, "score": outcome}, "trajectory": map[string]any{"scored": true, "score": traj},
			"calls": []any{}, "cost_usd": 0.05, "duration_ms": 1000,
		})
		sum += (3*tool + outcome + traj) / 5
	}
	mean := 0.0
	if len(cases) > 0 {
		mean = sum / float64(len(cases))
	}
	return map[string]any{
		"id": id, "project_id": "p1", "status": "finished",
		"setup": map[string]any{
			"fingerprint": fingerprint, "model": "opus-5-5", "cli_version": "2.1.14", "agents": 1, "skills": 6, "instructions": 11,
			// The instruction file's digest follows the fingerprint: a new fingerprint changed AGENTS.md.
			"parts": map[string]any{"agent": "a1", "model": "m1", "instruction AGENTS.md": fingerprint, "skill skills/verify-harness/SKILL.md": "k1"},
		},
		"cases": scored, "patterns": map[string]any{}, "aggregate": map[string]any{"scored": len(cases), "mean": mean},
		"cost_usd": cost, "duration_ms": 432000, "started_at": at, "finished_at": at,
	}
}

func (a *benchAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/bench/cases"):
		json.NewEncoder(w).Encode(map[string]any{"cases": a.cases})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/bench/scorecards"):
		json.NewEncoder(w).Encode(map[string]any{"scorecards": a.cards})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/compare"):
		a.compare(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bench/runs"):
		a.runsStart++
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{"id": "run9", "project_id": "p1", "status": "running", "total": len(a.cases), "done": 0, "started_at": "2026-11-26T12:00:00Z"})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bench/runs/run9/cancel"):
		a.cancels++
		json.NewEncoder(w).Encode(map[string]any{"id": "run9", "project_id": "p1", "status": "running", "total": len(a.cases), "done": 0, "started_at": "2026-11-26T12:00:00Z"})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/bench/runs/run9"):
		json.NewEncoder(w).Encode(map[string]any{"id": "run9", "project_id": "p1", "status": "failed", "total": len(a.cases), "done": 1, "error": a.runError, "started_at": "2026-11-26T12:00:00Z"})
	default:
		http.NotFound(w, r)
	}
}

// compare answers the compare endpoint as the server does: the scorecard against ?baseline=, else against the
// next older finished one (a.cards is newest first); 404 BENCH_NO_PREVIOUS_SCORECARD when there is none.
func (a *benchAPI) compare(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/compare"), "/")
	id, want := parts[len(parts)-1], r.URL.Query().Get("baseline")
	cur, base := -1, -1
	for i, c := range a.cards {
		if c["id"] == id {
			cur = i
		}
		if want != "" && c["id"] == want {
			base = i
		}
	}
	if cur < 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": "scorecard not found", "code": "BENCH_SCORECARD_NOT_FOUND"})
		return
	}
	if want == "" {
		for i := cur + 1; i < len(a.cards); i++ {
			if a.cards[i]["status"] == "finished" {
				base = i
				break
			}
		}
	}
	if base < 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": "there is no earlier scorecard to compare with", "code": "BENCH_NO_PREVIOUS_SCORECARD"})
		return
	}
	mean := func(c map[string]any) float64 { return c["aggregate"].(map[string]any)["mean"].(float64) }
	caseMean := func(c map[string]any) float64 {
		tu := c["tool_use"].(map[string]any)["right_tool"].(map[string]any)["score"].(float64)
		return (3*tu + c["outcome"].(map[string]any)["score"].(float64) + c["trajectory"].(map[string]any)["score"].(float64)) / 5
	}
	delta := func(before, after float64) map[string]any {
		return map[string]any{"before": before, "after": after, "change": after - before}
	}
	was := map[string]map[string]any{}
	for _, c := range a.cards[base]["cases"].([]map[string]any) {
		was[c["case_id"].(string)] = c
	}
	var cases []map[string]any
	for _, c := range a.cards[cur]["cases"].([]map[string]any) {
		p, ok := was[c["case_id"].(string)]
		switch {
		case !ok:
			cases = append(cases, map[string]any{"case_id": c["case_id"], "status": "added"})
		case caseMean(p) != caseMean(c):
			cases = append(cases, map[string]any{"case_id": c["case_id"], "status": "changed", "mean": delta(caseMean(p), caseMean(c))})
		default:
			cases = append(cases, map[string]any{"case_id": c["case_id"], "status": "unchanged"})
		}
	}
	var changed []string
	for part, digest := range a.cards[cur]["setup"].(map[string]any)["parts"].(map[string]any) {
		if a.cards[base]["setup"].(map[string]any)["parts"].(map[string]any)[part] != digest {
			changed = append(changed, part)
		}
	}
	json.NewEncoder(w).Encode(map[string]any{
		"changed_parts": append([]string{}, changed...),
		"previous_id": a.cards[base]["id"], "fingerprint_changed": a.cards[base]["setup"].(map[string]any)["fingerprint"] != a.cards[cur]["setup"].(map[string]any)["fingerprint"],
		"aggregate": delta(mean(a.cards[base]), mean(a.cards[cur])), "groups": map[string]any{}, "cases": cases,
	})
}

func (a *benchAPI) started() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runsStart
}

func scoreServer(t *testing.T, api http.Handler) *httptest.Server {
	t.Helper()
	return scoreServerWithFeed(t, api, nil)
}

// scoreServerWithFeed is scoreServer with the project's event feed (GET /api/events) in memory, when there is one.
func scoreServerWithFeed(t *testing.T, api http.Handler, feed http.Handler) *httptest.Server {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	w := board()
	mux := http.NewServeMux()
	mux.Handle("/api/projects/p1/bench/", api)
	if feed != nil {
		mux.Handle("/api/events", feed)
	}
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestScorePageInTheBrowser: the Scorecard reads the newest run beside the previous one with the tiles (cost
// beside score) and the case rows, the filter narrows them, the Runs view lists the runs and reading one opens
// the Scorecard on it, and Run bench starts a run through the API.
func TestScorePageInTheBrowser(t *testing.T) {
	api := &benchAPI{
		cases: []map[string]any{{"id": "a", "name": "a", "prompt": "a", "tools": []any{map[string]any{"name": "t"}}, "source": "definition"}, {"id": "b", "name": "b", "prompt": "b", "tools": []any{}, "source": "definition"}},
		cards: []map[string]any{
			benchCard("s2", "2026-11-26T11:40:00Z", "fp1", 1.14, [4]any{"a", 0.5, 0.5, 0.5}, [4]any{"b", 0.9, 0.9, 0.9}),
			benchCard("s1", "2026-11-25T18:02:00Z", "fp1", 1.09, [4]any{"a", 0.95, 0.9, 0.9}, [4]any{"b", 0.7, 0.8, 0.8}),
		},
	}
	srv := scoreServer(t, api)
	ctx, errs := browser(t)

	var toolbar, compare, tiles, rowsAll, rowsRegressed, runs, afterRead, noise string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/score"),
		chromedp.Poll(`!!document.querySelector('.score-tile')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('.toolbar').textContent`, &toolbar),
		chromedp.Evaluate(`document.querySelector('.score-compare').textContent`, &compare),
		chromedp.Evaluate(`[...document.querySelectorAll('.score-tile')].map(t => t.textContent).join('|')`, &tiles),
		chromedp.Evaluate(`document.querySelector('[data-noise]')?.textContent ?? ''`, &noise),
		chromedp.Evaluate(`[...document.querySelectorAll('.sc-row[data-case]')].map(r => r.dataset.case).join()`, &rowsAll),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label=Filter] button')].find(b => b.textContent.startsWith('Regressed')).click()`, nil),
		chromedp.Poll(`document.querySelectorAll('.sc-row[data-case]').length === 1`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.sc-row[data-case]').textContent`, &rowsRegressed),
		chromedp.FullScreenshot(&shot, 90),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label=View] button')].find(b => b.textContent.startsWith('Runs')).click()`, nil),
		chromedp.Poll(`document.querySelectorAll('.sc-row[data-run]').length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.sc-row[data-run]')].map(r => r.textContent).join('|')`, &runs),
		chromedp.Evaluate(`document.querySelector('.sc-row[data-run="s1"]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('.score-compare')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.score-compare').textContent`, &afterRead),
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Run bench').click()`, nil),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for _, want := range []string{"Score", "Scorecard", "Cases 2", "Runs 2", "last run 2", "Run bench"} {
		if !strings.Contains(toolbar, want) {
			t.Errorf("the toolbar %q lacks %q", toolbar, want)
		}
	}
	if !strings.Contains(compare, "run 2") || !strings.Contains(compare, "run 1") || !strings.Contains(compare, "setup") {
		t.Errorf("the compare bar reads %q", compare)
	}
	for _, want := range []string{"Aggregate score", "Estimated cost$1.14", "run 1 $1.09", "1 regressed · 1 improved"} {
		if !strings.Contains(tiles, want) {
			t.Errorf("the tiles %q lack %q", tiles, want)
		}
	}
	if !strings.Contains(noise, "Runs are not deterministic") {
		t.Errorf("same setup, score moved: banner %q", noise)
	}
	if !strings.HasPrefix(rowsAll, "a,b") {
		t.Errorf("rows %q", rowsAll)
	}
	if !strings.Contains(rowsRegressed, "regressed") && !strings.Contains(rowsRegressed, "▼") {
		t.Errorf("regressed row %q", rowsRegressed)
	}
	if !strings.Contains(runs, "run 2") || !strings.Contains(runs, "run 1") {
		t.Errorf("runs %q", runs)
	}
	if !strings.Contains(afterRead, "run 1") || !strings.Contains(afterRead, "no earlier finished run") {
		t.Errorf("after reading run 1 (a later run exists) the compare bar keeps its baseline menu and says there is no earlier finished run; it reads %q", afterRead)
	}
	deadline := time.Now().Add(3 * time.Second)
	for api.started() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if api.started() != 1 {
		t.Errorf("Run bench started %d runs, want 1", api.started())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/score.jpg", shot, 0o644)
	}
}

// TestScorePageWithNoCasesTeachesWhatACaseIs: nothing to score yet reads as an empty state, and Run bench is off.
func TestScorePageWithNoCasesTeachesWhatACaseIs(t *testing.T) {
	srv := scoreServer(t, &benchAPI{})
	ctx, errs := browser(t)
	var empty string
	var disabled bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/score"),
		chromedp.Poll(`!!document.querySelector('.empty')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('.empty').textContent`, &empty),
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Run bench').disabled`, &disabled),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(empty, "No bench cases yet") || !strings.Contains(empty, "A case is a prompt with mock tools") {
		t.Errorf("empty state %q", empty)
	}
	if !disabled {
		t.Error("Run bench is on with no case to score")
	}
}
