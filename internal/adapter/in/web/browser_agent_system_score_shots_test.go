package web

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// TestAgentSystemAndScoreScreenshots saves screenshots of the Agent system and Score pages in the states of the
// UX study, at 1440 px and 390 px, into WEB_SCREENSHOT_DIR, to put beside the approved mockups. Without the
// variable it does nothing.
func TestAgentSystemAndScoreScreenshots(t *testing.T) {
	dir := os.Getenv("WEB_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set WEB_SCREENSHOT_DIR to save the screenshots")
	}
	viewports := []struct {
		name string
		w, h int64
	}{{"desktop", 1440, 900}, {"390", 390, 844}}
	shoot := func(t *testing.T, ctx context.Context, name string) {
		t.Helper()
		var shot []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 90)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := os.WriteFile(dir+"/"+name+".jpg", shot, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// top saves what the window shows (not the stitched full page, where sticky parts land out of place).
	top := func(t *testing.T, ctx context.Context, name string) {
		t.Helper()
		var shot []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := os.WriteFile(dir+"/"+name+".png", shot, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ready := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	run := func(t *testing.T, ctx context.Context, errs *errorLog, actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("%v\nJS errors: %v", err, errs.all())
		}
	}
	// WEB_SCREENSHOT_SCHEME is light (the default; the mockups are light) or dark.
	scheme := os.Getenv("WEB_SCREENSHOT_SCHEME")
	if scheme == "" {
		scheme = "light"
	}
	look := func(w, h int64) chromedp.Action {
		return chromedp.Tasks{
			chromedp.EmulateViewport(w, h),
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}}),
		}
	}
	click := func(expr string) chromedp.Action { return chromedp.Evaluate(expr+`.click()`, nil) }
	view := func(name string) string {
		return `[...document.querySelectorAll('[aria-label=View] button')].find(b => b.textContent.startsWith('` + name + `'))`
	}

	// Agent system: a project with a few saved values; the first repository is missing a required gate.
	for _, v := range viewports {
		t.Run("agent-system-"+v.name, func(t *testing.T) {
			api := newAgentSystemAPI("harness", "blog")
			api.set("", "guard.retry_limit", 5)
			api.set("", "review.critic.model", "sonnet")
			api.set("blog", "guard.protected_paths", []string{"check.sh", "migrations/**"})
			api.set("blog", "learn.context_map.enabled", false)
			srv := agentSystemServer(t, api)
			ctx, errs := browser(t)
			name := "agent-system-" + v.name
			run(t, ctx, errs,
				look(v.w, v.h),
				chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
				ready(`!!document.querySelector('.ledger-row[data-gate]')`),
			)
			shoot(t, ctx, name+"-project")
			top(t, ctx, name+"-project-window")
			run(t, ctx, errs,
				chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=harness"),
				ready(`!!document.querySelector('.ledger-row[data-gate]')`),
			)
			shoot(t, ctx, name+"-repository-red")
			run(t, ctx, errs,
				chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=blog"),
				ready(`!!document.querySelector('.ledger-row[data-gate]')`),
				click(`document.querySelector('.ledger-row[data-path="learn.task_memory.require_update"] [role=switch]')`),
				ready(`!!document.querySelector('.save-bar')`),
			)
			shoot(t, ctx, name+"-unsaved")
			api.mu.Lock()
			api.refuse = []map[string]string{{"field": "learn.task_memory.require_update", "code": "INVALID_TYPE", "message": "takes true or false"}}
			api.mu.Unlock()
			run(t, ctx, errs,
				click(`document.querySelector('.save-bar .end button:last-child')`),
				ready(`!!document.querySelector('[data-refused]')`),
			)
			shoot(t, ctx, name+"-refused")
			run(t, ctx, errs,
				click(`document.querySelector('.ledger-row[data-path="learn.task_memory.require_update"] [role=switch]')`),
				click(`document.querySelector('.ledger-row[data-path="learn.context_map.enabled"] [role=switch]')`),
				ready(`!!document.querySelector('.save-bar')`),
				click(`document.querySelector('.save-bar .end button:last-child')`),
				ready(`document.body.textContent.includes('applies to sessions started afterwards')`),
			)
			shoot(t, ctx, name+"-saved")
		})
	}

	// Score: two scored runs, with the second regressed on one case.
	for _, v := range viewports {
		t.Run("score-"+v.name, func(t *testing.T) {
			api := &benchAPI{
				cases: []map[string]any{{"id": "a", "name": "a", "prompt": "a", "tools": []any{map[string]any{"name": "t"}}, "source": "definition"}, {"id": "b", "name": "b", "prompt": "b", "tools": []any{}, "source": "definition"}},
				cards: []map[string]any{
					benchCard("s2", "2026-11-26T11:40:00Z", "fp1", 1.14, [4]any{"a", 0.5, 0.5, 0.5}, [4]any{"b", 0.9, 0.9, 0.9}),
					benchCard("s1", "2026-11-25T18:02:00Z", "fp1", 1.09, [4]any{"a", 0.95, 0.9, 0.9}, [4]any{"b", 0.7, 0.8, 0.8}),
				},
				runError: "the claude CLI exited 1",
			}
			feed := &agentSystemStream{}
			srv := scoreServerWithFeed(t, api, feed)
			ctx, errs := browser(t)
			name := "score-" + v.name
			run(t, ctx, errs,
				look(v.w, v.h),
				chromedp.Navigate(srv.URL+"/projects/p1/score"),
				ready(`!!document.querySelector('.score-tile')`),
			)
			shoot(t, ctx, name+"-scorecard")
			top(t, ctx, name+"-scorecard-window")
			run(t, ctx, errs, click(view("Runs")), ready(`document.querySelectorAll('.sc-row[data-run]').length === 2`))
			shoot(t, ctx, name+"-runs")
			run(t, ctx, errs, click(view("Scorecard")), ready(`!!document.querySelector('.score-tile')`))
			for deadline := time.Now().Add(10 * time.Second); feed.followers() == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			}
			run(t, ctx, errs,
				click(`[...document.querySelectorAll('.toolbar button')].find(b => b.textContent === 'Run bench')`),
				ready(`!!document.querySelector('[data-running]')`),
			)
			feed.push(map[string]any{"bench_run": map[string]any{"run_id": "run9", "status": "running", "done": 1, "total": 2}})
			run(t, ctx, errs, ready(`document.querySelector('[data-running]').textContent.includes('1 of 2 cases')`))
			shoot(t, ctx, name+"-running")
			feed.push(map[string]any{"bench_run": map[string]any{"run_id": "run9", "status": "failed", "done": 1, "total": 2}})
			run(t, ctx, errs, ready(`!!document.querySelector('[data-failed-run]')`))
			shoot(t, ctx, name+"-failed")
		})
		t.Run("score-cases-"+v.name, func(t *testing.T) {
			api := &casesAPI{cases: []map[string]any{
				{"id": "a", "name": "a", "prompt": "Read the README.", "tools": []any{map[string]any{"name": "read_file", "result": "# Demo"}}, "expect": map[string]any{"tools": []any{map[string]any{"tool": "read_file"}}, "ordered": true}, "source": "definition"},
				{"id": "b", "name": "b", "prompt": "Fix the failing test.", "tools": []any{}, "expect": map[string]any{}, "source": "definition"},
			}}
			srv := scoreServer(t, api)
			ctx, errs := browser(t)
			run(t, ctx, errs,
				look(v.w, v.h),
				chromedp.Navigate(srv.URL+"/projects/p1/score"),
				ready(`!!document.querySelector('.score-tile')`),
				click(view("Cases")),
				ready(`!!document.querySelector('.sc-row[data-kind=case]')`),
				click(`document.querySelector('.sc-row[data-kind=case]')`),
				ready(`!!document.querySelector('.trace')`),
			)
			shoot(t, ctx, "score-"+v.name+"-cases-trace")
		})
		t.Run("score-empty-"+v.name, func(t *testing.T) {
			srv := scoreServer(t, &benchAPI{})
			ctx, errs := browser(t)
			run(t, ctx, errs,
				look(v.w, v.h),
				chromedp.Navigate(srv.URL+"/projects/p1/score"),
				ready(`document.body.textContent.includes('Run bench') && !document.body.textContent.includes('Loading')`),
			)
			shoot(t, ctx, "score-"+v.name+"-no-cases")
		})
	}
}
