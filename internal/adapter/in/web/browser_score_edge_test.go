package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// deletedCaseAPI is the cases API of a harness whose case was deleted from another tab: starting a run that names
// it is refused as the harness does (422 BENCH_CASE_IDS_INVALID).
type deletedCaseAPI struct{ *casesAPI }

func (a deletedCaseAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bench/runs") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"error": "not cases of this project: a", "code": "BENCH_CASE_IDS_INVALID"})
		return
	}
	a.casesAPI.ServeHTTP(w, r)
}

// Replay of a case that no longer exists: the refusal is shown naming it, no run banner appears, and Replay can be
// pressed again.
func TestScoreReplayOfADeletedCaseShowsTheRefusalInTheBrowser(t *testing.T) {
	inner := &casesAPI{cases: []map[string]any{{"id": "a", "name": "a", "prompt": "Read the README.", "tools": []any{map[string]any{"name": "read_file", "result": "# Demo"}}, "expect": map[string]any{"tools": []any{map[string]any{"tool": "read_file"}}, "ordered": true}, "source": "definition"}}}
	srv := scoreServer(t, deletedCaseAPI{inner})
	ctx, errs := browser(t)
	wait := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	var problem string
	var running, replayDisabled bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/score"),
		wait(`!!document.querySelector('.score-tile')`),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label=View] button')].find(b => b.textContent.startsWith('Cases')).click()`, nil),
		wait(`!!document.querySelector('.sc-row[data-kind=case]')`),
		chromedp.Evaluate(`document.querySelector('.sc-row[data-kind=case]').click()`, nil),
		wait(`!!document.querySelector('[data-action=replay]')`),
		chromedp.Evaluate(`document.querySelector('[data-action=replay]').click()`, nil),
		wait(`!!document.querySelector('[data-problem]')`),
		chromedp.Evaluate(`document.querySelector('[data-problem]').textContent`, &problem),
		chromedp.Evaluate(`!!document.querySelector('[data-running]')`, &running),
		chromedp.Evaluate(`document.querySelector('[data-action=replay]').disabled`, &replayDisabled),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(problem, "did not start") || !strings.Contains(problem, "not cases of this project") {
		t.Errorf("the refusal reads %q", problem)
	}
	if running {
		t.Errorf("a run banner is up for a run the harness refused")
	}
	if replayDisabled {
		t.Errorf("Replay stays disabled after the refusal")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// Cancel while a progress event for the same run arrives: the banner keeps counting and says it is cancelling, and
// the run's end event closes it.
func TestScoreProgressArrivingWhileCancellingKeepsTheBannerInTheBrowser(t *testing.T) {
	api := &benchAPI{
		cases: []map[string]any{{"id": "a", "name": "a", "prompt": "a", "tools": []any{}, "source": "definition"}, {"id": "b", "name": "b", "prompt": "b", "tools": []any{}, "source": "definition"}},
		cards: []map[string]any{benchCard("s1", "2026-11-25T18:02:00Z", "fp1", 1.09, [4]any{"a", 0.95, 0.9, 0.9}, [4]any{"b", 0.7, 0.8, 0.8})},
	}
	feed := &agentSystemStream{}
	srv := scoreServerWithFeed(t, api, feed)
	ctx, errs := browser(t)
	wait := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/score"),
		wait(`!!document.querySelector('.score-tile')`),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for deadline := time.Now().Add(10 * time.Second); feed.followers() == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
	}
	var banner string
	var stillRunning bool
	err := chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Run bench').click()`, nil),
		wait(`!!document.querySelector('[data-running]')`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	feed.push(map[string]any{"bench_run": map[string]any{"run_id": "run9", "status": "running", "done": 0, "total": 2}})
	err = chromedp.Run(ctx,
		wait(`!![...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Cancel')`),
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Cancel').click()`, nil),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	feed.push(map[string]any{"bench_run": map[string]any{"run_id": "run9", "status": "running", "done": 1, "total": 2}})
	err = chromedp.Run(ctx,
		wait(`document.querySelector('[data-running]')?.textContent.includes('1 of 2 cases')`),
		chromedp.Evaluate(`document.querySelector('[data-running]').textContent`, &banner),
		chromedp.Evaluate(`!!document.querySelector('[data-running]')`, &stillRunning),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !stillRunning || !strings.Contains(banner, "1 of 2 cases") {
		t.Errorf("banner %q", banner)
	}
	feed.push(map[string]any{"bench_run": map[string]any{"run_id": "run9", "status": "cancelled", "done": 1, "total": 2}})
	err = chromedp.Run(ctx,
		wait(`!document.querySelector('[data-running]')`),
		wait(`!![...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Run bench')`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if api.cancelled() != 1 {
		t.Errorf("cancel requests %d, want 1", api.cancelled())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// The Score page at 390 px does not scroll sideways, with a run in progress.
func TestScorePageOnAPhoneDoesNotScrollSidewaysInTheBrowser(t *testing.T) {
	api := &benchAPI{
		cases: []map[string]any{{"id": "a", "name": "a long case name that could push the row wider than a phone", "prompt": "a", "tools": []any{}, "source": "definition"}},
		cards: []map[string]any{
			benchCard("s1", "2026-11-25T18:02:00Z", "fp1", 1.09, [4]any{"a", 0.95, 0.9, 0.9}),
			benchCard("s2", "2026-11-26T11:40:00Z", "fp2", 1.14, [4]any{"a", 0.55, 0.5, 0.5}),
		},
	}
	srv := scoreServer(t, api)
	ctx, errs := browser(t)
	var wide bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(srv.URL+"/projects/p1/score"),
		chromedp.Poll(`!!document.querySelector('.score-tile')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.documentElement.scrollWidth > window.innerWidth + 1`, &wide),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if wide {
		t.Errorf("the Score page scrolls sideways at 390 px")
	}
}
