package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)


func asWait(expr string) chromedp.Action {
	return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond))
}

func asSetInput(selector, value string) chromedp.Action {
	return chromedp.Evaluate(`(() => { const el = document.querySelector(`+jsString(selector)+`); const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype; Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, `+toJSON(value)+`); el.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
}

func asRowOf(path string) string {
	return `document.querySelector('.ledger-row[data-path="` + path + `"]')`
}

// A project with one repository: the scope switch still offers it, selects it, and the address names it.
func TestAgentSystemOfAProjectWithOneRepositoryInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("only")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var options, address string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		asWait(`!!document.querySelector('.ledger-row[data-path]')`),
		chromedp.Evaluate(`document.querySelector('.toolbar [aria-label=Scope] button:last-child').click()`, nil),
		asWait(`!!document.querySelector('select[aria-label=Repository]')`),
		chromedp.Evaluate(`[...document.querySelectorAll('select[aria-label=Repository] option')].map(o => o.value).join()`, &options),
		chromedp.Evaluate(`new URL(location.href).searchParams.get('repository') ?? ''`, &address),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if options != "only" || address != "only" {
		t.Errorf("options %q, address %q, want only both times", options, address)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// A repository with no checkout: detection finds nothing and the check.sh preview is empty. Import says so, makes
// no draft and sends nothing; the red banner stays.
func TestAgentSystemImportOfARepositoryWithNoCheckoutChangesNothingInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var note string
	var bar bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=harness"),
		asWait(`!!document.querySelector('[data-import-check-sh]')`),
		chromedp.Evaluate(`document.querySelector('[data-import-check-sh]').click()`, nil),
		asWait(`!!document.querySelector('[data-import-note]')`),
		chromedp.Evaluate(`document.querySelector('[data-import-note]').textContent`, &note),
		chromedp.Evaluate(`!!document.querySelector('.save-bar')`, &bar),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if strings.TrimSpace(note) == "" {
		t.Errorf("the import note is empty")
	}
	if bar || len(api.patchLog()) != 0 {
		t.Errorf("an empty import made a draft (bar %v) or a save %v", bar, api.patchLog())
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// Override then Reset on the same field before saving leaves nothing unsaved and sends nothing.
func TestAgentSystemOverrideThenResetBeforeSavingSendsNothingInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	row := asRowOf("learn.context_map.enabled")
	var action1, action2, source string
	var bar bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=blog"),
		asWait(`!!document.querySelector('.ledger-row[data-path]')`),
		chromedp.Evaluate(row+`.querySelector('.act').textContent`, &action1),
		chromedp.Evaluate(row+`.querySelector('.act button').click()`, nil),
		asWait(`!!document.querySelector('.save-bar')`),
		chromedp.Evaluate(row+`.querySelector('.act').textContent`, &action2),
		chromedp.Evaluate(row+`.querySelector('.act button').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('.save-bar')`, &bar),
		chromedp.Evaluate(row+`.querySelector('.src').textContent`, &source),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if action1 != "Override" || action2 != "Reset" {
		t.Errorf("actions %q then %q, want Override then Reset", action1, action2)
	}
	if bar {
		t.Errorf("the save bar stays after the override was reset")
	}
	if !strings.Contains(source, "harness default") {
		t.Errorf("the row reads %q, want it back to its inherited source", source)
	}
	if len(api.patchLog()) != 0 {
		t.Errorf("patches %v, want none", api.patchLog())
	}
}

// A 422 INVALID_GLOB on a list lands on the list's row, names the glob, keeps the chip the person typed, and Save
// stays disabled until the list changes.
func TestAgentSystemRefusedGlobInAListKeepsTheChipInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var rowErr, chips string
	var disabled bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		asWait(`!!document.querySelector('input[aria-label="Add to Protected paths"]')`),
		asSetInput(`input[aria-label="Add to Protected paths"]`, "src/[bad"),
		chromedp.Evaluate(`document.querySelector('input[aria-label="Add to Protected paths"]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))`, nil),
		asWait(`!!document.querySelector('.save-bar')`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	api.mu.Lock()
	api.refuse = []map[string]string{{"field": "guard.protected_paths", "code": "INVALID_GLOB", "message": "src/[bad is not a glob"}}
	api.mu.Unlock()
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		asWait(`!!document.querySelector('[data-error-for="guard.protected_paths"]')`),
		chromedp.Evaluate(`document.querySelector('[data-error-for="guard.protected_paths"]').textContent`, &rowErr),
		chromedp.Evaluate(`[...document.querySelectorAll('.ledger-row[data-path="guard.protected_paths"] .badge.glob')].map(b => b.textContent).join()`, &chips),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').getAttribute('aria-disabled') === 'true'`, &disabled),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(rowErr, "INVALID_GLOB") || !strings.Contains(rowErr, "src/[bad") {
		t.Errorf("row error %q", rowErr)
	}
	if !strings.Contains(chips, "src/[bad") {
		t.Errorf("the chip the person typed is gone: %q", chips)
	}
	if !disabled {
		t.Errorf("Save is enabled while the refusal stands")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// A visual QA block half filled (URL and routes emptied): the page must not send a declaration without a URL.
func TestAgentSystemHalfFilledVisualQAIsNeverSentInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var disabled bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=blog"),
		asWait(`!!document.querySelector('[data-visual-qa]')`),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b => b.textContent.includes('Declare this repository')).click()`, nil),
		asWait(`!!document.querySelector('input[aria-label="URL"]')`),
		asSetInput(`input[aria-label="URL"]`, ""),
		asSetInput(`textarea[aria-label="Routes"]`, ""),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').getAttribute('aria-disabled') === 'true'`, &disabled),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for _, p := range api.patchLog() {
		if strings.Contains(p, `"url":""`) || strings.Contains(p, `"routes":[]`) {
			t.Errorf("a half-filled declaration was saved: %s (Save disabled: %v)", p, disabled)
		}
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// Keyboard only: the scope switch and a switch row take Space or Enter, and the save bar's Save takes Enter.
func TestAgentSystemWorksFromTheKeyboardAloneInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	row := asRowOf("learn.context_map.enabled")
	var scopeAfter, checked string
	var focusable string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		asWait(`!!document.querySelector('.ledger-row[data-path]')`),
		// The scope switch: focus the Repository button and press Enter.
		chromedp.Evaluate(`document.querySelector('.toolbar [aria-label=Scope] button:last-child').focus()`, nil),
		chromedp.KeyEvent(kb.Enter),
		asWait(`!!document.querySelector('select[aria-label=Repository]')`),
		chromedp.Evaluate(`new URL(location.href).searchParams.get('repository') ?? ''`, &scopeAfter),
		// A switch row: focus and Space.
		chromedp.Evaluate(row+`.querySelector('[role=switch]').tabIndex + ''`, &focusable),
		chromedp.Evaluate(row+`.querySelector('[role=switch]').focus()`, nil),
		chromedp.KeyEvent(" "),
		asWait(`!!document.querySelector('.save-bar')`),
		chromedp.Evaluate(row+`.querySelector('[role=switch]').getAttribute('aria-checked')`, &checked),
		// Save: focus and Enter.
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').focus()`, nil),
		chromedp.KeyEvent(kb.Enter),
		asWait(`!document.querySelector('.save-bar')`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if scopeAfter != "harness" {
		t.Errorf("Enter on the scope switch left ?repository= at %q", scopeAfter)
	}
	if focusable != "0" && focusable != "-1" {
		t.Errorf("switch tabIndex %q", focusable)
	}
	if checked != "false" {
		t.Errorf("Space on a switch left it at %q, want false", checked)
	}
	if patches := api.patchLog(); len(patches) != 1 || !strings.Contains(patches[0], `"learn.context_map.enabled":false`) {
		t.Errorf("patches %v, want one save of the keyboard toggle", patches)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// Two tabs on the same field: the second tab, which has its own unsaved value, hears the first save, keeps its
// draft, and its Save is the last writer.
func TestAgentSystemTwoTabsSavingTheSameFieldInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctxA, errsA := browser(t)
	retry := `document.querySelector('.ledger-row[data-path="guard.retry_limit"] input')`
	if err := chromedp.Run(ctxA,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		asWait(`!!`+retry),
		asSetInput(`.ledger-row[data-path="guard.retry_limit"] input`, "5"),
		asWait(`!!document.querySelector('.save-bar')`),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errsA.all())
	}
	ctxB, cancelB := chromedp.NewContext(ctxA)
	defer cancelB()
	if err := chromedp.Run(ctxB,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		asWait(`!!`+retry),
	); err != nil {
		t.Fatalf("tab B: %v", err)
	}
	for deadline := time.Now().Add(10 * time.Second); api.stream.followers() < 1; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the pages never followed the feed")
		}
	}
	// Tab B saves 8; the harness announces it.
	if err := chromedp.Run(ctxB,
		asSetInput(`.ledger-row[data-path="guard.retry_limit"] input`, "8"),
		asWait(`!!document.querySelector('.save-bar')`),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		asWait(`!document.querySelector('.save-bar')`),
	); err != nil {
		t.Fatalf("tab B save: %v", err)
	}
	api.stream.push(map[string]any{"agent_system_settings": map[string]any{"project_id": "p1"}})
	var banner, draft string
	if err := chromedp.Run(ctxA,
		asWait(`!!document.querySelector('[data-changed-elsewhere]')`),
		chromedp.Evaluate(`document.querySelector('[data-changed-elsewhere]').textContent`, &banner),
		chromedp.Evaluate(retry+`.value`, &draft),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		asWait(`!document.querySelector('.save-bar')`),
	); err != nil {
		t.Fatalf("tab A: %v\nJS errors: %v", err, errsA.all())
	}
	if draft != "5" {
		t.Errorf("tab A's draft became %q when the other tab saved, want 5 kept", draft)
	}
	patches := api.patchLog()
	if len(patches) != 2 || !strings.Contains(patches[0], `"guard.retry_limit":8`) || !strings.Contains(patches[1], `"guard.retry_limit":5`) {
		t.Errorf("patches %v, want 8 from tab B then 5 from tab A", patches)
	}
	if e := errsA.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// At 390 px a refused save with its banner, in a repository scope, still does not scroll sideways, and the save bar
// stays inside the viewport.
func TestAgentSystemRefusedSaveOnAPhoneDoesNotScrollSidewaysInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var wide bool
	var barRight float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=harness"),
		asWait(`!!document.querySelector('.ledger-row[data-path]')`),
		asSetInput(`.ledger-row[data-path="guard.retry_limit"] input`, "11"),
		asWait(`!!document.querySelector('.save-bar') && !!document.querySelector('[data-error-for="guard.retry_limit"]')`),
		chromedp.Evaluate(`document.documentElement.scrollWidth > window.innerWidth + 1`, &wide),
		chromedp.Evaluate(`document.querySelector('.save-bar').getBoundingClientRect().right`, &barRight),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if wide {
		t.Errorf("the page scrolls sideways at 390 px with a refusal and the missing-gate banner up")
	}
	if barRight > 391 {
		t.Errorf("the save bar ends at %v px, beyond the viewport", barRight)
	}
}
