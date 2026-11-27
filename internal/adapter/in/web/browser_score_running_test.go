package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func (a *benchAPI) cancelled() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancels
}

// TestScoreRunningBannerCancelAndFailedRunInTheBrowser: a started run shows the signal banner with its progress
// (from the project feed) and Cancel in place of Run bench, Cancel asks the harness to stop it, and a run that ends
// failed shows the danger banner with why and a Retry that starts a new run.
func TestScoreRunningBannerCancelAndFailedRunInTheBrowser(t *testing.T) {
	api := &benchAPI{
		cases: []map[string]any{{"id": "a", "name": "a", "prompt": "a", "tools": []any{}, "source": "definition"}, {"id": "b", "name": "b", "prompt": "b", "tools": []any{}, "source": "definition"}},
		cards: []map[string]any{
			benchCard("s1", "2026-11-25T18:02:00Z", "fp1", 1.09, [4]any{"a", 0.95, 0.9, 0.9}, [4]any{"b", 0.7, 0.8, 0.8}),
		},
		runError: "the claude CLI exited 1",
	}
	feed := &agentSystemStream{}
	srv := scoreServerWithFeed(t, api, feed)
	ctx, errs := browser(t)

	var running, toolbarRunning, failed, percent string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/score"),
		chromedp.Poll(`!!document.querySelector('.score-compare, [data-notice], .score-tile')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for deadline := time.Now().Add(10 * time.Second); feed.followers() == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
	}
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Run bench').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[data-running]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	feed.push(map[string]any{"bench_run": map[string]any{"run_id": "run9", "status": "running", "done": 1, "total": 2}})
	err = chromedp.Run(ctx,
		chromedp.Poll(`document.querySelector('[data-running]').textContent.includes('1 of 2 cases')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-running]').textContent`, &running),
		chromedp.Evaluate(`document.querySelector('[data-running] [role=progressbar]').getAttribute('aria-valuenow')`, &percent),
		chromedp.Evaluate(`document.querySelector('.toolbar .end').textContent`, &toolbarRunning),
		chromedp.Evaluate(`[...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Cancel').click()`, nil),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for deadline := time.Now().Add(3 * time.Second); api.cancelled() == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
	}
	if api.cancelled() != 1 {
		t.Errorf("Cancel asked the harness %d times, want 1", api.cancelled())
	}
	if !strings.Contains(running, "Run bench") || !strings.Contains(running, "1 of 2 cases") {
		t.Errorf("the running banner reads %q", running)
	}
	if percent != "50" {
		t.Errorf("the bar is at %q, want 50", percent)
	}
	if strings.Contains(toolbarRunning, "Run bench") || !strings.Contains(toolbarRunning, "Cancel") {
		t.Errorf("the toolbar while running reads %q: Cancel replaces Run bench", toolbarRunning)
	}

	feed.push(map[string]any{"bench_run": map[string]any{"run_id": "run9", "status": "failed", "done": 1, "total": 2}})
	err = chromedp.Run(ctx,
		chromedp.Poll(`!!document.querySelector('[data-failed-run]') && !document.querySelector('[data-running]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-failed-run]').textContent`, &failed),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-failed-run] button')].find(b => b.textContent === 'Retry').click()`, nil),
		chromedp.Poll(`!document.querySelector('[data-failed-run]') && !!document.querySelector('[data-running]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(failed, "failed") || !strings.Contains(failed, "the claude CLI exited 1") {
		t.Errorf("the failed banner reads %q", failed)
	}
	if api.started() != 2 {
		t.Errorf("Retry started %d runs in all, want 2", api.started())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
